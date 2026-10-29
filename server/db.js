import Database from 'better-sqlite3';
import { createHash, randomBytes } from 'node:crypto';
import { mkdirSync, chmodSync } from 'node:fs';
import { dirname } from 'node:path';
import { normalize, estimate, Decimal, pricingNote } from './usage.js';
import { provenance, observe } from './provenance.js';
export const hash = (s) => createHash('sha256').update(s).digest('hex');
export const secret = () => randomBytes(32).toString('base64url');
export function openDb(path, now = Date.now()) {
  if (path !== ':memory:') {
    mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  }
  const db = new Database(path);
  if (path !== ':memory:') chmodSync(path, 0o600);
  db.pragma('journal_mode = WAL');
  db.pragma('busy_timeout = 5000');
  db.pragma('foreign_keys = ON');
  db.exec(`CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY,identity TEXT UNIQUE NOT NULL,fingerprint TEXT NOT NULL,occurred_at INTEGER NOT NULL,agent TEXT NOT NULL,model TEXT NOT NULL,data TEXT NOT NULL);
    CREATE INDEX IF NOT EXISTS events_time ON events(occurred_at,agent,model);
    CREATE TABLE IF NOT EXISTS cursors(path TEXT PRIMARY KEY,data TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS contexts(turn TEXT PRIMARY KEY,model TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS errors(id TEXT PRIMARY KEY,kind TEXT NOT NULL,at TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS accesses(hash TEXT PRIMARY KEY,expires INTEGER NOT NULL,used INTEGER NOT NULL DEFAULT 0);
    CREATE TABLE IF NOT EXISTS sessions(hash TEXT PRIMARY KEY,expires INTEGER NOT NULL,csrf TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS native_observations(identity TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,observed_at TEXT NOT NULL,data TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS text_measurements(identity TEXT PRIMARY KEY,occurred_at INTEGER NOT NULL,data TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS attempts(key TEXT PRIMARY KEY,count INTEGER NOT NULL,until INTEGER NOT NULL);`);
  db.prepare('INSERT OR IGNORE INTO meta VALUES (?,?)').run('activation', String(now));
  return db;
}
export function recordError(db, kind, identity = '') {
  db.prepare('INSERT OR IGNORE INTO errors VALUES (?,?,?)').run(
    hash(kind + identity),
    kind,
    new Date().toISOString(),
  );
}
export function insertEvent(db, input) {
  const e = normalize(input),
    canonical = JSON.stringify(e),
    fingerprint = hash(canonical),
    identity = hash(e.provider + ':' + e.source + ':' + e.responseId);
  const prior = db.prepare('SELECT fingerprint,data FROM events WHERE identity=?').get(identity);
  observe(db, identity, { ...e, ...provenance(input) }, fingerprint);
  if (input.method === 'native')
    db.prepare('DELETE FROM text_measurements WHERE identity=?').run(
      hash(e.source + ':' + e.responseId),
    );
  if (prior) {
    if (prior.fingerprint !== fingerprint) {
      recordError(db, 'response_conflict', identity);
      return 'conflict';
    }
    if (input.method === 'native') {
      const old = JSON.parse(prior.data),
        calc = estimate({ ...e, ...provenance(input) }),
        components = {};
      if (calc.costUsd === old.costUsd)
        for (const k of [
          'costCacheReadUsd',
          'costCacheWriteUsd',
          'costVisibleOutputUsd',
          'costReasoningUsd',
        ])
          components[k] = calc[k] ?? null;
      db.prepare('UPDATE events SET data=? WHERE identity=?').run(
        JSON.stringify({ ...old, ...components, ...provenance(input) }),
        identity,
      );
    }
    return 'duplicate';
  }
  db.prepare(
    'INSERT INTO events(identity,fingerprint,occurred_at,agent,model,data) VALUES (?,?,?,?,?,?)',
  ).run(
    identity,
    fingerprint,
    Date.parse(e.occurredAt),
    e.agent,
    e.model,
    JSON.stringify({ ...e, ...estimate({ ...e, ...provenance(input) }), ...provenance(input) }),
  );
  return 'inserted';
}
export function issue(db, now = Date.now()) {
  const token = secret(),
    expires = now + 8 * 3600_000;
  db.transaction(() => {
    db.prepare('DELETE FROM accesses').run();
    db.prepare('DELETE FROM sessions').run();
    db.prepare('INSERT INTO accesses(hash,expires) VALUES (?,?)').run(hash(token), expires);
  }).immediate();
  return { token, expiresAt: new Date(expires).toISOString() };
}
export function revoke(db) {
  db.transaction(() => {
    db.prepare('DELETE FROM accesses').run();
    db.prepare('DELETE FROM sessions').run();
  }).immediate();
}
export function exchange(db, token, now = Date.now()) {
  return db
    .transaction(() => {
      const a = db
        .prepare(
          'UPDATE accesses SET used=1 WHERE hash=? AND used=0 AND expires>? RETURNING expires',
        )
        .get(hash(token), now);
      if (!a) return null;
      const session = secret(),
        csrf = secret();
      db.prepare('INSERT INTO sessions VALUES (?,?,?)').run(hash(session), a.expires, csrf);
      return { session, csrf, expiresAt: new Date(a.expires).toISOString() };
    })
    .immediate();
}
export const windows = { '15m': 900000, '1h': 3600000, '24h': 86400000, '7d': 604800000 };
export function filters(q, now = Date.now()) {
  const window = q.window ?? '1h';
  if (!windows[window]) throw Error('invalid_window');
  const to = q.to === undefined ? now : Number(q.to);
  if (!Number.isSafeInteger(to) || to < 0 || to > now + 60000) throw Error('invalid_to');
  const params = [to - windows[window], to];
  let where = 'occurred_at>=? AND occurred_at<=?';
  for (const k of ['agent', 'model', 'provider', 'sessionId', 'method'])
    if (q[k]) {
      if (typeof q[k] !== 'string' || q[k].length > 160) throw Error('invalid_filter');
      where += ` AND ${['agent', 'model'].includes(k) ? k : `json_extract(data,'$.${k}')`}=?`;
      params.push(q[k]);
    }
  return {
    where,
    params,
    from: params[0],
    to,
    window,
    selected: Object.fromEntries(
      ['agent', 'model', 'provider', 'sessionId', 'method'].map((k) => [k, q[k] ?? '']),
    ),
  };
}
export function events(db, f, { limit = 50, offset = 0 } = {}) {
  limit = Number(limit);
  offset = Number(offset);
  if (
    !Number.isInteger(limit) ||
    limit < 1 ||
    limit > 100 ||
    !Number.isInteger(offset) ||
    offset < 0
  )
    throw Error('invalid_pagination');
  const rows = db
    .prepare(
      `SELECT id,data FROM events WHERE ${f.where} ORDER BY occurred_at DESC,id DESC LIMIT ? OFFSET ?`,
    )
    .all(...f.params, limit, offset);
  return {
    events: rows.map((r) => ({ ...JSON.parse(r.data), id: r.id })),
    total: db.prepare(`SELECT COUNT(*) AS n FROM events WHERE ${f.where}`).get(...f.params).n,
    offset,
    limit,
    to: f.to,
  };
}
const empty = () => ({
  count: 0,
  inputTokens: 0n,
  outputTokens: 0n,
  cachedInputTokens: 0n,
  cacheWriteInputTokens: 0n,
  reasoningOutputTokens: 0n,
  normalInputTokens: 0n,
  visibleOutputTokens: 0n,
  costCacheReadUsd: new Decimal(0),
  costCacheWriteUsd: new Decimal(0),
  costVisibleOutputUsd: new Decimal(0),
  costReasoningUsd: new Decimal(0),
  costInputUsd: new Decimal(0),
  costCacheUsd: new Decimal(0),
  costOutputUsd: new Decimal(0),
  missingCache: 0,
  missingReasoning: 0,
  unpriced: 0,
  costUsd: new Decimal(0),
});
function add(a, e) {
  a.count++;
  if (e.cachedInputTokens !== null && e.cacheWriteInputTokens !== null)
    a.normalInputTokens +=
      BigInt(e.inputTokens) - BigInt(e.cachedInputTokens) - BigInt(e.cacheWriteInputTokens);
  if (e.reasoningOutputTokens !== null)
    a.visibleOutputTokens += BigInt(e.outputTokens) - BigInt(e.reasoningOutputTokens);
  for (const k of [
    'costInputUsd',
    'costCacheUsd',
    'costOutputUsd',
    'costCacheReadUsd',
    'costCacheWriteUsd',
    'costVisibleOutputUsd',
    'costReasoningUsd',
  ])
    if (e[k] != null) a[k] = a[k].plus(e[k]);
  for (const k of [
    'inputTokens',
    'outputTokens',
    'cachedInputTokens',
    'cacheWriteInputTokens',
    'reasoningOutputTokens',
  ])
    a[k] += BigInt(e[k] ?? 0);
  if (e.cachedInputTokens === null || e.cacheWriteInputTokens === null) a.missingCache++;
  if (e.reasoningOutputTokens === null) a.missingReasoning++;
  if (e.costUsd === null) a.unpriced++;
  else a.costUsd = a.costUsd.plus(e.costUsd);
}
function json(a) {
  return {
    ...a,
    ...Object.fromEntries(
      ['costCacheReadUsd', 'costCacheWriteUsd', 'costVisibleOutputUsd', 'costReasoningUsd'].map(
        (k) => [k, a[k].toFixed()],
      ),
    ),
    normalInputTokens: String(a.normalInputTokens),
    visibleOutputTokens: String(a.visibleOutputTokens),
    costInputUsd: a.costInputUsd.toFixed(),
    costCacheUsd: a.costCacheUsd.toFixed(),
    costOutputUsd: a.costOutputUsd.toFixed(),
    inputTokens: String(a.inputTokens),
    outputTokens: String(a.outputTokens),
    totalTokens: String(a.inputTokens + a.outputTokens),
    cachedInputTokens: String(a.cachedInputTokens),
    cacheWriteInputTokens: String(a.cacheWriteInputTokens),
    reasoningOutputTokens: String(a.reasoningOutputTokens),
    costUsd: a.costUsd.toFixed(),
  };
}
export function analytics(db, f) {
  const totals = empty(),
    step = windows[f.window] / 60,
    buckets = Array.from({ length: 60 }, (_, i) => ({
      ...empty(),
      at: new Date(f.from + i * step).toISOString(),
    })),
    agents = new Map(),
    models = new Map(),
    agentSeries = new Map();
  for (const r of db
    .prepare(`SELECT data,occurred_at FROM events WHERE ${f.where} ORDER BY occurred_at,id`)
    .iterate(...f.params)) {
    const e = JSON.parse(r.data),
      index = Math.min(59, Math.floor((r.occurred_at - f.from) / step));
    add(totals, e);
    add(buckets[index], e);
    if (!agentSeries.has(e.agent))
      agentSeries.set(e.agent, {
        lastEventAt: e.occurredAt,
        buckets: Array.from({ length: 60 }, (_, i) => ({
          at: new Date(f.from + i * step).toISOString(),
          count: 0,
          inputTokens: 0n,
          outputTokens: 0n,
          costUsd: new Decimal(0),
        })),
      });
    const series = agentSeries.get(e.agent),
      bucket = series.buckets[index];
    series.lastEventAt = e.occurredAt;
    bucket.count++;
    bucket.inputTokens += BigInt(e.inputTokens);
    bucket.outputTokens += BigInt(e.outputTokens);
    if (e.costUsd !== null) bucket.costUsd = bucket.costUsd.plus(e.costUsd);
    for (const [map, key] of [
      [agents, e.agent],
      [models, e.model],
    ]) {
      if (!map.has(key)) map.set(key, empty());
      add(map.get(key), e);
    }
  }
  let accumulated = new Decimal(0);
  return {
    from: f.from,
    to: f.to,
    window: f.window,
    intervalMs: step,
    totals: json(totals),
    buckets: buckets.map((b) => {
      accumulated = accumulated.plus(b.costUsd);
      return { ...json(b), cumulativeCostUsd: accumulated.toFixed() };
    }),
    agents: [...agents].map(([name, v]) => ({ name, ...json(v) })),
    models: [...models].map(([name, v]) => ({ name, ...json(v) })),
    agentPanels: [...agents].map(([name, v]) => ({
      name,
      ...json(v),
      lastEventAt: agentSeries.get(name).lastEventAt,
      buckets: agentSeries.get(name).buckets.map((b) => ({
        ...b,
        inputTokens: String(b.inputTokens),
        outputTokens: String(b.outputTokens),
        costUsd: b.costUsd.toFixed(),
      })),
    })),
    options: Object.fromEntries(
      [
        ['agents', 'agent'],
        ['models', 'model'],
        ['providers', 'provider'],
        ['sessions', 'sessionId'],
        ['methods', 'method'],
      ].map(([name, key]) => [
        name,
        db
          .prepare(
            `SELECT DISTINCT json_extract(data,'$.${key}') AS name FROM (SELECT data FROM events UNION ALL SELECT data FROM text_measurements) WHERE name IS NOT NULL ORDER BY name`,
          )
          .all()
          .map((r) => r.name),
      ]),
    ),
    pricingNote,
  };
}
