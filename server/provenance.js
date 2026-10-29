import { createHash } from 'node:crypto';
const digest = (x) => createHash('sha256').update(x).digest('hex');
const short = (x) =>
  typeof x === 'string' && x.length <= 160 && !/[\x00-\x1f]/.test(x) ? x : null;
export function provenance(input) {
  const p = {
    method: input.method === 'native' ? 'native' : 'reported',
    sessionId: short(input.sessionId),
    turnId: short(input.turnId),
    sourceVersion: short(input.sourceVersion),
    coverage: short(input.coverage) ?? 'reported_usage',
    modality: short(input.modality) ?? 'unknown',
    pricingVersion: '2026-10-29.v1',
    serviceTier: 'standard_assumed',
  };
  return p;
}
export function observe(db, identity, input, fingerprint) {
  if (input.method !== 'native') return;
  const p = provenance(input),
    counts = {};
  for (const k of [
    'inputTokens',
    'outputTokens',
    'cachedInputTokens',
    'cacheWriteInputTokens',
    'reasoningOutputTokens',
    'totalTokens',
  ])
    counts[k] = input[k] ?? null;
  const previous = db
    .prepare('SELECT fingerprint FROM native_observations WHERE identity=?')
    .get(identity);
  const accepted = db.prepare('SELECT fingerprint FROM events WHERE identity=?').get(identity);
  if (previous && accepted && previous.fingerprint !== accepted.fingerprint) return; // Keep the first conflict visible until reviewed.
  db.prepare(
    'INSERT INTO native_observations(identity,fingerprint,observed_at,data) VALUES (?,?,?,?) ON CONFLICT(identity) DO UPDATE SET fingerprint=excluded.fingerprint,observed_at=excluded.observed_at,data=excluded.data',
  ).run(identity, fingerprint, new Date().toISOString(), JSON.stringify({ ...p, ...counts }));
}
export function reconcile(db, f) {
  const rows = db
    .prepare(
      `SELECT e.identity,e.fingerprint,e.data,o.fingerprint AS observed,o.observed_at FROM (SELECT * FROM events WHERE ${f.where}) e LEFT JOIN native_observations o ON o.identity=e.identity ORDER BY e.occurred_at DESC,e.id DESC`,
    )
    .all(...f.params);
  const totals = { responses: rows.length, matched: 0, conflicts: 0, unverified: 0 };
  let lastObserved = null;
  const sessions = new Map();
  for (const r of rows) {
    const e = JSON.parse(r.data),
      key = e.sessionId ?? 'unknown';
    if (!sessions.has(key))
      sessions.set(key, {
        sessionId: key,
        count: 0,
        inputTokens: 0n,
        outputTokens: 0n,
        cachedInputTokens: 0n,
      });
    const s = sessions.get(key);
    s.count++;
    for (const k of ['inputTokens', 'outputTokens', 'cachedInputTokens']) s[k] += BigInt(e[k] ?? 0);
    if (!r.observed) totals.unverified++;
    else if (r.observed === r.fingerprint) totals.matched++;
    else totals.conflicts++;
    if (r.observed_at && (!lastObserved || r.observed_at > lastObserved))
      lastObserved = r.observed_at;
  }
  const diagnostics = db
    .prepare(
      'SELECT data FROM text_measurements WHERE occurred_at>=? AND occurred_at<=? ORDER BY occurred_at DESC',
    )
    .all(f.from, f.to)
    .map((r) => JSON.parse(r.data))
    .filter((e) => Object.entries(f.selected ?? {}).every(([k, v]) => !v || e[k] === v))
    .slice(0, 100);
  return {
    ...totals,
    lastObserved,
    sessions: [...sessions.values()].map((s) => ({
      ...s,
      inputTokens: String(s.inputTokens),
      outputTokens: String(s.outputTokens),
      cachedInputTokens: String(s.cachedInputTokens),
    })),
    textMeasurements: diagnostics,
    note: 'Conciliación por respuesta contra el contador nativo leído. El contexto repetido pertenece a cada llamada. La estimación local cubre solo texto visible y no sustituye ni se suma al consumo nativo.',
  };
}
export function saveMeasurement(db, input, result) {
  if (
    db
      .prepare('SELECT 1 FROM events WHERE identity=?')
      .get(digest(input.provider + ':' + input.source + ':' + input.responseId))
  )
    return;
  const key = digest(input.source + ':' + input.responseId);
  const data = {
    source: short(input.source),
    provider: short(input.provider),
    agent: short(input.agent),
    model: short(input.model),
    sessionId: short(input.sessionId),
    responseId: short(input.responseId),
    occurredAt: new Date(input.occurredAt).toISOString(),
    method: 'local_text',
    coverage: 'visible_output_text_only',
    status: result.status,
    tokens: result.tokens ?? null,
    tokenizer: result.tokenizer ?? null,
    version: result.version ?? null,
    vocabulary: result.vocabulary ?? null,
    comparison: 'not_comparable_to_full_native_usage',
  };
  db.prepare(
    'INSERT INTO text_measurements VALUES (?,?,?) ON CONFLICT(identity) DO UPDATE SET data=excluded.data,occurred_at=excluded.occurred_at',
  ).run(key, Date.parse(data.occurredAt), JSON.stringify(data));
}
