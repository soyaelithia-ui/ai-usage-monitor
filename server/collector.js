import { readdirSync, statSync, openSync, readSync, closeSync, watch, existsSync } from 'node:fs';
import { join } from 'node:path';
import JSONbig from 'json-bigint';
import { hash, insertEvent, recordError } from './db.js';
import { catalogMessage } from './adapters.js';
import { agents } from './catalog.js';
const parse = JSONbig({
  storeAsString: true,
  protoAction: 'error',
  constructorAction: 'error',
}).parse;
function files(root, pattern) {
  const out = [];
  for (const d of readdirSync(root, { withFileTypes: true })) {
    const p = join(root, d.name);
    if (d.isDirectory()) out.push(...files(p, pattern));
    else if (d.isFile() && pattern.test(d.name)) out.push(p);
  }
  return out.sort();
}
function signature(fd, offset) {
  if (!offset) return '';
  const b = Buffer.alloc(Math.min(128, offset));
  readSync(fd, b, 0, b.length, offset - b.length);
  return hash(b);
}
export class Collector {
  constructor(db, root, agent, onChange = () => {}) {
    this.db = db;
    this.root = root;
    // The declarative catalogue entry drives everything: markers, record
    // types, the model context and the usage paths. No per-agent branches.
    this.agent = agent ?? agents.find((a) => a.reader === 'jsonl');
    if (!this.agent) throw Error('Sin agente JSONL en el catálogo');
    this.pattern = new RegExp(this.agent.filePattern);
    this.onChange = onChange;
    this.status = { state: 'starting', lastScan: null, lastEvent: null, files: 0 };
  }
  scan() {
    if (!this.root) {
      this.status = {
        state: 'waiting',
        reason: 'Ruta de registros no configurada',
        lastScan: new Date().toISOString(),
        lastEvent: null,
        files: 0,
      };
      return false;
    }
    let changed = false;
    this.scanFailed = false;
    try {
      const list = files(this.root, this.pattern);
      this.status.files = list.length;
      for (const path of list) changed = this.read(path) || changed;
      this.status.state = this.scanFailed ? 'error' : 'running';
      this.status.lastScan = new Date().toISOString();
    } catch {
      this.status.state = 'error';
      recordError(this.db, 'collector_read_failed');
    }
    if (changed) this.onChange();
    return changed;
  }
  read(path) {
    let fd;
    try {
      const st = statSync(path);
      fd = openSync(path, 'r');
      const saved = this.db.prepare('SELECT data FROM cursors WHERE path=?').get(path);
      let c = saved ? JSON.parse(saved.data) : { offset: 0, inode: String(st.ino), sig: '' };
      if (
        c.inode !== String(st.ino) ||
        st.size < c.offset ||
        signature(fd, c.offset) !== c.sig ||
        (c.mtimeMs !== undefined && st.mtimeMs !== c.mtimeMs && st.size <= c.observedSize)
      )
        c = { offset: 0, inode: String(st.ino), sig: '' };
      if (st.size === c.offset) return false;
      // Bounded reads; only complete newline-terminated records advance the durable cursor.
      const b = Buffer.alloc(Math.min(st.size - c.offset, 16 * 1024 * 1024));
      const n = readSync(fd, b, 0, b.length, c.offset);
      const end = b.subarray(0, n).lastIndexOf(10);
      if (end < 0) {
        if (n === 16 * 1024 * 1024) {
          this.scanFailed = true;
          recordError(this.db, 'oversized_or_partial_record', path + ':' + c.offset);
        }
        return false;
      }
      let changed = false;
      this.db
        .transaction(() => {
          const activation = Number(
            this.db.prepare("SELECT value FROM meta WHERE key='activation'").get().value,
          );
          let byteOffset = c.offset;
          for (const line of b
            .subarray(0, end + 1)
            .toString('utf8')
            .split('\n')) {
            if (!line) continue;
            try {
              // Do not deserialize or persist conversation content.
              if (!this.agent.markers.some((marker) => line.includes(marker))) {
                byteOffset += Buffer.byteLength(line) + 1;
                continue;
              }
              const r = parse(line);
              const ctx = this.agent.context;
              if (ctx && (r.type === ctx.recordType || r.payload?.type === ctx.recordType)) {
                const turn = this.pluck(r, ctx.key),
                  model = this.pluck(r, ctx.model);
                if (typeof turn === 'string' && typeof model === 'string')
                  this.db
                    .prepare(
                      'INSERT INTO contexts VALUES (?,?) ON CONFLICT(turn) DO UPDATE SET model=excluded.model',
                    )
                    .run(turn, model);
              }
              const type = r.type ?? r.payload?.type;
              if (this.agent.recordTypes?.includes(type)) {
                const time = Date.parse(this.pluck(r, this.agent.usage.occurredAt));
                if (!Number.isFinite(time)) throw Error('invalid_timestamp');
                if (time >= activation) {
                  const e = catalogMessage(this.agent, r, {
                    model: ctx
                      ? (this.db
                          .prepare('SELECT model FROM contexts WHERE turn=?')
                          .get(this.pluck(r, ctx.key) ?? '')?.model ?? 'unknown')
                      : 'unknown',
                  });
                  const result = insertEvent(this.db, e);
                  if (result === 'inserted') {
                    changed = true;
                    this.status.lastEvent = r.timestamp;
                  }
                }
              }
            } catch {
              recordError(this.db, 'invalid_usage_record', path + ':' + byteOffset);
            }
            byteOffset += Buffer.byteLength(line) + 1;
          }
          c.offset += end + 1;
          c.sig = signature(fd, c.offset);
          c.mtimeMs = st.mtimeMs;
          c.observedSize = st.size;
          this.db
            .prepare(
              'INSERT INTO cursors VALUES (?,?) ON CONFLICT(path) DO UPDATE SET data=excluded.data',
            )
            .run(path, JSON.stringify(c));
        })
        .immediate();
      return changed;
    } catch {
      recordError(this.db, 'collector_file_failed', path);
      this.scanFailed = true;
      this.status.state = 'error';
      return false;
    } finally {
      if (fd !== undefined) closeSync(fd);
    }
  }
  pluck(record, path) {
    let value = record;
    for (const key of path.split('.')) {
      if (value === null || typeof value !== 'object') return undefined;
      value = value[key];
    }
    return value;
  }
  start() {
    if (this.root && !this.db.prepare("SELECT 1 FROM meta WHERE key='provenance_v2'").get()) {
      this.db
        .transaction(() => {
          this.db.prepare('DELETE FROM cursors WHERE path LIKE ?').run(this.root + '/%');
          this.db.prepare("INSERT INTO meta VALUES ('provenance_v2','1')").run();
        })
        .immediate();
    }
    this.scan();
    if (this.root && existsSync(this.root)) {
      try {
        this.watcher = watch(this.root, { recursive: true }, () => {
          clearTimeout(this.watchDebounce);
          this.watchDebounce = setTimeout(() => this.scan(), 50);
        });
        this.watcher.on('error', () => {});
      } catch {}
    }
    this.timer = setInterval(() => this.scan(), 1000);
    return this;
  }
  stop() {
    clearInterval(this.timer);
    clearTimeout(this.watchDebounce);
    if (this.watcher) {
      try {
        this.watcher.close();
      } catch {}
      this.watcher = null;
    }
  }
}
