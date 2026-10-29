import test from 'node:test';
import assert from 'node:assert/strict';
import { openDb, insertEvent, analytics, filters } from '../server/db.js';
const epoch = Date.parse('2026-10-29T18:00:00Z');
const row = (id, agent, changes = {}) => ({
  responseId: String(id),
  source: 'test',
  provider: 'openai',
  model: 'gpt-6-astra',
  agent,
  occurredAt: new Date(epoch - id * 1000).toISOString(),
  inputTokens: '9007199254740993',
  outputTokens: '20',
  cachedInputTokens: '1',
  cacheWriteInputTokens: '0',
  reasoningOutputTokens: '2',
  ...changes,
});
test('agent sections aggregate every page with exact independent series and last consumption', () => {
  const db = openDb(':memory:', epoch - 3600000);
  for (let i = 0; i < 165; i++) insertEvent(db, row(i, i % 3 ? 'Codex' : 'Antigravity'));
  const result = analytics(db, filters({ window: '1h' }, epoch));
  assert.equal(result.agentPanels.length, 2);
  const codex = result.agentPanels.find((a) => a.name === 'Codex'),
    agy = result.agentPanels.find((a) => a.name === 'Antigravity');
  assert.equal(codex.count, 110);
  assert.equal(agy.count, 55);
  assert.equal(codex.inputTokens, (9007199254740993n * 110n).toString());
  assert.equal(
    codex.buckets.reduce((n, b) => n + b.count, 0),
    110,
  );
  assert.equal(
    codex.buckets.reduce((n, b) => n + BigInt(b.inputTokens), 0n).toString(),
    codex.inputTokens,
  );
  assert.equal(
    agy.buckets.reduce((n, b) => n + BigInt(b.outputTokens), 0n).toString(),
    agy.outputTokens,
  );
  assert.equal(agy.lastEventAt, new Date(epoch).toISOString());
  assert.equal(codex.lastEventAt, new Date(epoch - 1000).toISOString());
  const filtered = analytics(db, filters({ window: '1h', agent: 'Antigravity' }, epoch));
  assert.equal(filtered.agentPanels.length, 1);
  assert.equal(filtered.agentPanels[0].count, 55);
  assert.equal(filtered.totals.count, 55);
  assert.equal(
    analytics(db, filters({ window: '1h', agent: 'OpenCode' }, epoch)).agentPanels.length,
    0,
  );
  db.close();
});
test('agent panels obey model/provider/session filters and count new calls once', () => {
  const db = openDb(':memory:', epoch - 3600000);
  insertEvent(db, row(1, 'Codex', { sessionId: 'one', method: 'native' }));
  insertEvent(
    db,
    row(2, 'Antigravity', {
      provider: 'google',
      model: 'gemini-2.5-pro',
      sessionId: 'two',
      method: 'native',
    }),
  );
  const q = filters(
    { window: '1h', provider: 'openai', sessionId: 'one', method: 'native' },
    epoch,
  );
  assert.equal(analytics(db, q).agentPanels[0].count, 1);
  const next = row(3, 'Codex', { sessionId: 'one', method: 'native' });
  insertEvent(db, next);
  insertEvent(db, next);
  assert.equal(analytics(db, q).agentPanels[0].count, 2);
  assert.equal(
    analytics(db, filters({ window: '1h', model: 'gemini-2.5-pro' }, epoch)).agentPanels[0].name,
    'Antigravity',
  );
  db.close();
});
