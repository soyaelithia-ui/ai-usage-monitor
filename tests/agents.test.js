import test from 'node:test';
import assert from 'node:assert/strict';
import request from 'supertest';
import {
  openDb,
  insertEvent,
  analytics,
  filters,
  getDynamicAgents,
  issue,
  exchange,
} from '../server/db.js';
import { createApp, cookieName } from '../server/app.js';
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
test('dynamic agents register via API and auto-register via contract ingestion', async () => {
  const db = openDb(':memory:');
  const { app, close } = createApp({
    db,
    origin: 'https://usage.test',
    gatewayKey: 'gw-key',
    ingestKey: 'ingest-secret',
    dist: process.cwd() + '/public',
  });
  try {
    const regRes = await request(app)
      .post('/api/agents/register')
      .set('authorization', 'Bearer ingest-secret')
      .send({
        id: 'swarm-agent',
        name: 'Swarm Agent',
        reader: 'contract',
        provider: 'custom',
        tone: 'amber',
        symbol: 'S',
      });
    assert.equal(regRes.status, 200);
    assert.equal(regRes.body.ok, true);

    const dynamicAgents = getDynamicAgents(db);
    assert.equal(
      dynamicAgents.some((a) => a.id === 'swarm-agent'),
      true,
    );

    const ingestRes = await request(app)
      .post('/api/ingest')
      .set('authorization', 'Bearer ingest-secret')
      .send({
        source: 'autonomous-coder',
        agent: 'Autonomous Coder',
        schema: 'usage-monitor.usage.v1',
        semantics: 'input_includes_cache;output_includes_reasoning',
        provider: 'deepseek',
        model: 'deepseek-4.1-coder',
        id: 'resp-123',
        occurredAt: new Date().toISOString(),
        inputTokens: 1000,
        outputTokens: 250,
      });
    assert.equal(ingestRes.status, 200);

    const updatedDynamic = getDynamicAgents(db);
    assert.equal(
      updatedDynamic.some((a) => a.id === 'autonomous-coder'),
      true,
    );

    // Register an agent with custom model rates (omitting cached rate to test defaulting)
    const ratedRes = await request(app)
      .post('/api/agents/register')
      .set('authorization', 'Bearer ingest-secret')
      .send({
        id: 'rated-agent',
        name: 'Rated Agent',
        reader: 'contract',
        provider: 'custom-provider',
        rates: {
          'custom-llm-v1': {
            input: '2.0',
            output: '4.0',
          },
        },
      });
    assert.equal(ratedRes.status, 200);

    // Ingest event for that model and verify price estimation uses the dynamic rates
    const ratedIngest = await request(app)
      .post('/api/ingest')
      .set('authorization', 'Bearer ingest-secret')
      .send({
        source: 'rated-agent',
        schema: 'usage-monitor.usage.v1',
        semantics: 'input_includes_cache;output_includes_reasoning',
        provider: 'custom-provider',
        model: 'custom-llm-v1',
        id: 'rated-resp-1',
        occurredAt: new Date().toISOString(),
        inputTokens: 1000000,
        outputTokens: 500000,
        cachedInputTokens: 0,
        cacheWriteInputTokens: 0,
        reasoningOutputTokens: 0,
      });
    assert.equal(ratedIngest.status, 200);

    // Verify the event in the DB has cost calculated based on rates (1M input @ $2/M + 0.5M output @ $4/M = $4.00)
    const eventRow = db.prepare('SELECT data FROM events WHERE model=?').get('custom-llm-v1');
    assert.ok(eventRow);
    const parsedData = JSON.parse(eventRow.data);
    assert.equal(parsedData.costUsd, '4');

    // Invalid agent ID should return 400
    const invalidRes = await request(app)
      .post('/api/agents/register')
      .set('authorization', 'Bearer ingest-secret')
      .send({
        id: 'invalid id with spaces!',
      });
    assert.equal(invalidRes.status, 400);

    // Discovery endpoint should return list of local technologies when authenticated
    const s = exchange(db, issue(db).token);
    const discRes = await request(app)
      .get('/api/discovery')
      .set('x-usage-monitor-gateway', 'gw-key')
      .set('cookie', cookieName + '=' + s.session);
    assert.equal(discRes.status, 200);
    assert.ok(Array.isArray(discRes.body.technologies));
    assert.ok(discRes.body.technologies.some((t) => t.id === 'opencode'));
  } finally {
    close();
    db.close();
  }
});

test('unseen agents in incoming events auto-register dynamically', () => {
  const db = openDb(':memory:');
  insertEvent(db, {
    source: 'custom-scanner',
    provider: 'anthropic',
    agent: 'Claude Worker',
    model: 'claude-3-7-sonnet',
    responseId: 'test-auto-reg-1',
    occurredAt: new Date().toISOString(),
    inputTokens: '500',
    outputTokens: '120',
  });
  const dyn = getDynamicAgents(db);
  const found = dyn.find((a) => a.name === 'Claude Worker');
  assert.ok(found, 'Claude Worker should be auto-registered in dynamic_agents');
  assert.equal(found.id, 'claude-worker');
  assert.equal(found.provider, 'anthropic');
  assert.ok(found.tone);
  assert.equal(found.symbol, 'C');
  db.close();
});

test('dynamic agents register with rose/cyan tones and custom iconUrl via API', async () => {
  const db = openDb(':memory:');
  const { app, close } = createApp({
    db,
    origin: 'https://usage.test',
    gatewayKey: 'gw-key',
    ingestKey: 'ing-key',
    dist: process.cwd() + '/public',
  });
  try {
    const res = await request(app)
      .post('/api/agents/register')
      .set('authorization', 'Bearer ing-key')
      .send({
        id: 'gemini-coder',
        name: 'Gemini Coder',
        reader: 'contract',
        provider: 'google',
        tone: 'cyan',
        symbol: 'G',
        iconUrl: 'https://example.com/gemini.svg',
      });
    assert.equal(res.status, 200);
    assert.equal(res.body.ok, true);

    const dyn = getDynamicAgents(db);
    const gemini = dyn.find((a) => a.id === 'gemini-coder');
    assert.ok(gemini);
    assert.equal(gemini.tone, 'cyan');
    assert.equal(gemini.config?.iconUrl, 'https://example.com/gemini.svg');

    // Sources endpoint returns the registered agent with its tone and iconUrl
    const s = exchange(db, issue(db).token);
    const sourcesRes = await request(app)
      .get('/api/sources')
      .set('x-usage-monitor-gateway', 'gw-key')
      .set('cookie', cookieName + '=' + s.session);
    assert.equal(sourcesRes.status, 200);
    const returnedAgent = sourcesRes.body.agents.find((a) => a.id === 'gemini-coder');
    assert.ok(returnedAgent);
    assert.equal(returnedAgent.tone, 'cyan');
    assert.equal(returnedAgent.iconUrl, 'https://example.com/gemini.svg');
  } finally {
    close();
    db.close();
  }
});
