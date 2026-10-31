import Database from 'better-sqlite3';
import { existsSync, readdirSync, statSync, watch } from 'node:fs';
import { join } from 'node:path';
import JSONbig from 'json-bigint';
import { insertEvent, recordError, hash, getDynamicAgents } from './db.js';
import { catalogMessage, visibleText } from './adapters.js';
import { agents } from './catalog.js';
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
  constructor(db, paths = {}, tokenizer, onChange = () => {}) {
    this.db = db;
    // Paths come keyed by the catalogue `configKey` of every declared agent.
    this.paths = paths;
    this.tokenizer = tokenizer;
    this.onChange = onChange;
    this.activation = Number(
      db.prepare("SELECT value FROM meta WHERE key='activation'").get().value,
    );
    this.status = {};
    for (const a of agents) {
      this.status[a.id] = { state: 'starting' };
    }
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
  fallback(e, entry, text) {
    if (
      !Number.isFinite(Date.parse(e.occurredAt)) ||
      Date.parse(e.occurredAt) < this.activation ||
      !e.model ||
      !e.responseId
    )
      return;
    if (text === null) return;
    this.tokenizer?.enqueue(
      {
        source: entry.id,
        sessionId: e.sessionId,
        responseId: e.responseId,
        occurredAt: e.occurredAt,
        provider: e.provider,
        model: e.model,
        agent: entry.name,
      },
      text,
    );
  }
  opencode() {
    const entry = agents.find((a) => a.reader === 'sqlite');
    const path = this.paths[entry.configKey];
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
      const rows = source.prepare(entry.query.sql).all(at, at, id);
      this.db
        .transaction(() => {
          for (const row of rows) {
            try {
              if (Buffer.byteLength(row.data) > maxFile) throw Error('opencode_record_limit');
              const m = parse(row.data);
              if (m.id !== row.id) throw Error('opencode_id_mismatch');
              if (row[entry.query.columns.version] !== entry.expectedVersion)
                throw Error('opencode_unsupported_version');
              const e = catalogMessage(entry, m, {
                sessionId: row[entry.query.columns.sessionId],
              });
              this.accept(e);
              if (!e && entry.fallback?.contentPath) {
                const occurredAt = new Date(Number(m.time?.completed ?? m.time?.created));
                if (Number.isFinite(occurredAt.getTime()))
                  this.fallback(
                    {
                      sessionId: row[entry.query.columns.sessionId],
                      responseId: m.id,
                      occurredAt: occurredAt.toISOString(),
                      provider: m.model?.providerID,
                      model: m.model?.id,
                    },
                    entry,
                    visibleText(m[entry.fallback.contentPath]),
                  );
              }
            } catch (e) {
              recordError(
                this.db,
                [
                  'opencode_unsupported_version',
                  'opencode_missing_model',
                  'opencode_missing_provider',
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
            this.save(path, {
              inode: String(st.ino),
              at: Number(last[entry.query.columns.updated]),
              id: last[entry.query.columns.id],
            });
          }
        })
        .immediate();
      // Once caught up, reread the last timestamp on the next poll (dedup is durable).
      if (rows.length < 200 && rows.length) {
        const last = rows.at(-1);
        this.save(path, {
          inode: String(st.ino),
          at: Math.max(0, Number(last[entry.query.columns.updated]) - 1),
          id: '',
        });
      }
      this.status.opencode = {
        state: 'running',
        version: entry.expectedVersion,
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
  antigravity() {
    const entry = agents.find((a) => a.reader === 'contract');
    const root = this.paths[entry.configKey],
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
    const allAgents = [...agents];
    const dyn = getDynamicAgents(this.db);
    for (const d of dyn) {
      if (!allAgents.some((a) => a.id === d.id)) allAgents.push(d);
    }
    for (const agent of allAgents) {
      if (!this.status[agent.id]) {
        this.status[agent.id] = { state: 'starting' };
      }
      try {
        if (typeof this[agent.id] === 'function') {
          this[agent.id]();
        } else if (agent.reader === 'sqlite' && agent.query) {
          const path = agent.config?.path || this.paths[agent.configKey || agent.id];
          if (!path || !existsSync(path)) {
            this.status[agent.id] = { state: 'waiting', reason: 'Sin base de datos configurada' };
          } else {
            this.status[agent.id] = {
              state: 'running',
              lastScan: new Date().toISOString(),
            };
          }
        } else if (agent.reader === 'contract') {
          this.status[agent.id] = {
            state: 'running',
            lastScan: new Date().toISOString(),
          };
        }
      } catch {
        this.status[agent.id] = { state: 'error' };
        recordError(this.db, agent.id + '_scan_failed');
      }
    }
    if (this.changed) this.onChange();
  }
  setupWatchers() {
    this.closeWatchers();
    this.watchers = [];
    for (const p of Object.values(this.paths || {})) {
      if (p && existsSync(p)) {
        try {
          const w = watch(p, () => {
            clearTimeout(this.watchDebounce);
            this.watchDebounce = setTimeout(() => this.scan(), 50);
          });
          w.on('error', () => {});
          this.watchers.push(w);
        } catch {}
      }
    }
  }
  closeWatchers() {
    clearTimeout(this.watchDebounce);
    if (this.watchers) {
      for (const w of this.watchers) {
        try {
          w.close();
        } catch {}
      }
      this.watchers = [];
    }
  }
  start() {
    this.scan();
    this.setupWatchers();
    this.timer = setInterval(() => this.scan(), 1000);
    return this;
  }
  stop() {
    clearInterval(this.timer);
    this.closeWatchers();
  }
}
