import Database from 'better-sqlite3';
import {
  existsSync,
  readdirSync,
  statSync,
  readFileSync,
  openSync,
  readSync,
  closeSync,
} from 'node:fs';
import { join } from 'node:path';
import JSONbig from 'json-bigint';
import { insertEvent, recordError, hash } from './db.js';
import { openCodeMessage, geminiMessage, visibleText } from './adapters.js';
const parse = JSONbig({
  storeAsString: true,
  protoAction: 'error',
  constructorAction: 'error',
}).parse;
const maxFile = 16 * 1024 * 1024;
function files(root, match, depth = 0) {
  if (!existsSync(root)) return [];
  if (depth > 5) return [];
  return readdirSync(root, { withFileTypes: true }).flatMap((d) =>
    d.isDirectory()
      ? files(join(root, d.name), match, depth + 1)
      : d.isFile() && match.test(d.name)
        ? [join(root, d.name)]
        : [],
  );
}
export class Sources {
  constructor(db, { openCodeDb, geminiRoot, antigravityRoot }, tokenizer, onChange = () => {}) {
    this.db = db;
    this.paths = { openCodeDb, geminiRoot, antigravityRoot };
    this.tokenizer = tokenizer;
    this.onChange = onChange;
    this.activation = Number(
      db.prepare("SELECT value FROM meta WHERE key='activation'").get().value,
    );
    this.status = {
      opencode: { state: 'starting' },
      gemini: { state: 'starting' },
      antigravity: { state: 'starting' },
    };
  }
  cursor(path) {
    return JSON.parse(
      this.db.prepare('SELECT data FROM cursors WHERE path=?').get(path)?.data ?? '{}',
    );
  }
  save(path, data) {
    this.db
      .prepare(
        'INSERT INTO cursors VALUES (?,?) ON CONFLICT(path) DO UPDATE SET data=excluded.data',
      )
      .run(path, JSON.stringify(data));
  }
  accept(e) {
    if (!e || Date.parse(e.occurredAt) < this.activation) return;
    const result = insertEvent(this.db, e);
    if (result === 'inserted') this.changed = true;
  }
  fallback(m, source, sessionId, occurredAt, provider, model, agent) {
    if (
      !Number.isFinite(Date.parse(occurredAt)) ||
      Date.parse(occurredAt) < this.activation ||
      !model ||
      !m.id
    )
      return;
    const text = visibleText(m.content);
    if (text === null) return;
    this.tokenizer?.enqueue(
      { source, sessionId, responseId: m.id, occurredAt, provider, model, agent },
      text,
    );
  }
  opencode() {
    const path = this.paths.openCodeDb;
    if (!path || !existsSync(path)) {
      this.status.opencode = { state: 'waiting', reason: 'Sin base de datos' };
      return;
    }
    let source;
    try {
      source = new Database(path, { readonly: true, fileMustExist: true, timeout: 50 });
      source.pragma('query_only=ON');
      source.defaultSafeIntegers(true);
      const c = this.cursor(path),
        st = statSync(path);
      let at = c.at ?? 0,
        id = c.id ?? '';
      if (c.inode !== String(st.ino)) {
        at = 0;
        id = '';
      }
      // A bounded overlap catches same-millisecond updates; (timestamp,id) makes large batches advance.
      const rows = source
        .prepare(
          `SELECT m.id,m.session_id,m.time_updated,m.data,s.version FROM session_message m JOIN session_v2 s ON s.id=m.session_id WHERE m.type IN ('assistant','compaction') AND (m.time_updated>? OR (m.time_updated=? AND m.id>?)) ORDER BY m.time_updated,m.id LIMIT 200`,
        )
        .all(at, at, id);
      this.db
        .transaction(() => {
          for (const row of rows) {
            try {
              if (Buffer.byteLength(row.data) > maxFile) throw Error('opencode_record_limit');
              const m = parse(row.data);
              if (m.id !== row.id) throw Error('opencode_id_mismatch');
              const e = openCodeMessage(m, row.session_id, row.version);
              this.accept(e);
              if (!m.tokens && m.time?.completed)
                this.fallback(
                  m,
                  'opencode',
                  row.session_id,
                  new Date(Number(m.time.completed)).toISOString(),
                  m.model?.providerID,
                  m.model?.id,
                  'OpenCode',
                );
            } catch (e) {
              recordError(
                this.db,
                [
                  'opencode_unsupported_version',
                  'opencode_missing_model',
                  'opencode_id_mismatch',
                  'opencode_record_limit',
                ].includes(e.message)
                  ? e.message
                  : 'opencode_invalid_usage',
                row.id,
              );
            }
          }
          if (rows.length) {
            const last = rows.at(-1);
            this.save(path, { inode: String(st.ino), at: Number(last.time_updated), id: last.id });
          }
        })
        .immediate();
      // Once caught up, reread the last timestamp on the next poll (dedup is durable).
      if (rows.length < 200 && rows.length) {
        const last = rows.at(-1);
        this.save(path, {
          inode: String(st.ino),
          at: Math.max(0, Number(last.time_updated) - 1),
          id: '',
        });
      }
      this.status.opencode = {
        state: 'running',
        version: '2.1.4',
        lastScan: new Date().toISOString(),
        recordsRead: rows.length,
      };
    } catch {
      this.status.opencode = { state: 'error', reason: 'Esquema o lectura no compatible' };
      recordError(this.db, 'opencode_read_failed');
    } finally {
      source?.close();
    }
  }
  gemini() {
    const root = this.paths.geminiRoot;
    const list = root ? files(root, /^session-.*\.jsonl?$/) : [];
    this.status.gemini = {
      state: list.length ? 'running' : 'waiting',
      files: list.length,
      lastScan: new Date().toISOString(),
    };
    for (const path of list) {
      try {
        if (path.endsWith('.jsonl')) {
          this.geminiJsonl(path);
          continue;
        }
        const st = statSync(path),
          c = this.cursor(path);
        if (c.size === st.size && c.mtime === st.mtimeMs && c.inode === String(st.ino)) continue;
        if (st.size > maxFile) throw Error('gemini_file_limit');
        const raw = readFileSync(path, 'utf8');
        let sessionId,
          messages = [];
        if (path.endsWith('.json')) {
          const r = parse(raw);
          if (!Array.isArray(r.messages) || typeof r.sessionId !== 'string')
            throw Error('gemini_schema');
          sessionId = r.sessionId;
          messages = r.messages;
        } else {
          const end = raw.lastIndexOf('\n');
          if (end < 0) continue;
          for (const line of raw.slice(0, end).split('\n')) {
            if (!line.trim()) continue;
            const r = parse(line);
            if (r.sessionId) sessionId = r.sessionId;
            if (r.$set?.sessionId) sessionId = r.$set.sessionId;
            if (r.$set?.messages) messages.push(...r.$set.messages);
            if (r.type === 'gemini') messages.push(r);
          }
        }
        if (typeof sessionId !== 'string') throw Error('gemini_schema');
        this.db
          .transaction(() => {
            for (const m of messages) {
              try {
                this.accept(geminiMessage(m, sessionId));
                if (
                  m.type === 'gemini' &&
                  !m.tokens &&
                  !messages.some((x) => x.id === m.id && x.tokens)
                )
                  this.fallback(
                    m,
                    'gemini-cli',
                    sessionId,
                    m.timestamp,
                    'google',
                    m.model,
                    'Gemini CLI',
                  );
              } catch (e) {
                recordError(
                  this.db,
                  e.message === 'gemini_total_mismatch' ? e.message : 'gemini_incomplete_usage',
                  path + ':' + m.id,
                );
              }
            }
            this.save(path, { inode: String(st.ino), size: st.size, mtime: st.mtimeMs });
          })
          .immediate();
      } catch (e) {
        this.status.gemini.state = e.name === 'SyntaxError' ? 'incomplete' : 'error';
        if (e.name !== 'SyntaxError')
          recordError(
            this.db,
            e.message === 'gemini_file_limit' ? e.message : 'gemini_file_incomplete',
            hash(path),
          );
      }
    }
  }
  geminiJsonl(path) {
    const st = statSync(path);
    let c = this.cursor(path),
      fd = openSync(path, 'r');
    const signature = (offset) => {
      if (!offset) return '';
      const b = Buffer.alloc(Math.min(128, offset));
      readSync(fd, b, 0, b.length, offset - b.length);
      return hash(b);
    };
    try {
      if (
        c.inode !== String(st.ino) ||
        !Number.isInteger(c.offset) ||
        st.size < c.offset ||
        signature(c.offset) !== c.sig ||
        (c.mtime !== st.mtimeMs && st.size <= c.size)
      )
        c = { offset: 0, inode: String(st.ino) };
      if (st.size === c.offset) return;
      const b = Buffer.alloc(Math.min(maxFile, st.size - c.offset)),
        n = readSync(fd, b, 0, b.length, c.offset),
        end = b.subarray(0, n).lastIndexOf(10);
      if (end < 0) {
        if (n === maxFile) throw Error('gemini_file_limit');
        return;
      }
      this.db
        .transaction(() => {
          let offset = c.offset;
          for (const line of b
            .subarray(0, end + 1)
            .toString('utf8')
            .split('\n')) {
            if (!line) continue;
            try {
              const r = parse(line);
              if (typeof r.sessionId === 'string' && r.sessionId.length <= 160)
                c.sessionId = r.sessionId;
              if (typeof r.$set?.sessionId === 'string' && r.$set.sessionId.length <= 160)
                c.sessionId = r.$set.sessionId;
              const messages =
                r.type === 'gemini' ? [r] : Array.isArray(r.$set?.messages) ? r.$set.messages : [];
              for (const m of messages) {
                if (!c.sessionId) throw Error('gemini_schema');
                this.accept(geminiMessage(m, c.sessionId));
                if (m.type === 'gemini' && !m.tokens)
                  this.fallback(
                    m,
                    'gemini-cli',
                    c.sessionId,
                    m.timestamp,
                    'google',
                    m.model,
                    'Gemini CLI',
                  );
              }
            } catch (e) {
              recordError(
                this.db,
                e.message === 'gemini_total_mismatch' ? e.message : 'gemini_incomplete_usage',
                path + ':' + offset,
              );
            }
            offset += Buffer.byteLength(line) + 1;
          }
          c.offset += end + 1;
          c.sig = signature(c.offset);
          c.size = st.size;
          c.mtime = st.mtimeMs;
          this.save(path, c);
        })
        .immediate();
    } finally {
      closeSync(fd);
    }
  }
  antigravity() {
    const root = this.paths.antigravityRoot,
      list = root ? files(root, /\.db$/) : [];
    let records = 0;
    for (const path of list) {
      let source;
      try {
        source = new Database(path, { readonly: true, fileMustExist: true, timeout: 50 });
        source.pragma('query_only=ON');
        records += source.prepare('SELECT count(*) n FROM gen_metadata').get().n;
        records += source.prepare('SELECT count(*) n FROM steps').get().n;
      } catch {
        this.status.antigravity = { state: 'unsupported', reason: 'Esquema no verificable' };
        return;
      } finally {
        source?.close();
      }
    }
    this.status.antigravity = {
      state: records ? 'unsupported' : 'waiting',
      reason: records
        ? 'Registros binarios sin esquema de uso verificable'
        : 'Sin registros de uso',
      files: list.length,
    };
  }
  scan() {
    this.changed = false;
    for (const name of ['opencode', 'gemini', 'antigravity']) {
      try {
        this[name]();
      } catch {
        this.status[name] = { state: 'error' };
        recordError(this.db, name + '_scan_failed');
      }
    }
    if (this.changed) this.onChange();
  }
  start() {
    this.scan();
    this.timer = setInterval(() => this.scan(), 2000);
    return this;
  }
  stop() {
    clearInterval(this.timer);
  }
}
