import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, appendFileSync, renameSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import request from 'supertest';
import {
  openDb,
  insertEvent,
  analytics,
  events,
  filters,
  issue,
  exchange,
  revoke,
  hash,
} from '../server/db.js';
import { normalize, estimate, antigravity } from '../server/usage.js';
import { Collector } from '../server/collector.js';
import { createApp, cookieName } from '../server/app.js';
const epoch = Date.parse('2026-10-29T06:00:00Z');
const sample = (changes = {}) => ({
  source: 'codex',
  provider: 'openai',
  agent: 'Codex',
  model: 'gpt-6-astra',
  responseId: 'r1',
  occurredAt: new Date(epoch).toISOString(),
  inputTokens: '100',
  outputTokens: '20',
  cachedInputTokens: '40',
  cacheWriteInputTokens: '10',
  reasoningOutputTokens: '5',
  ...changes,
});
test('cache and reasoning are subsets; exact decimal cost and huge integers', () => {
  const e = normalize(sample());
  assert.equal(e.totalTokens, '120');
  assert.equal(estimate(e).costUsd, '0.001665');
  assert.equal(estimate(e).costInputUsd, '0.0005');
  const big = normalize(
    sample({
      inputTokens: '9007199254740993123456789',
      cachedInputTokens: '0',
      cacheWriteInputTokens: '0',
    }),
  );
  assert.equal(big.totalTokens, '9007199254740993123456809');
  assert.equal(estimate(big).costInputUsd, '180143985094819862469.13578');
  assert.throws(() => normalize(sample({ inputTokens: 9007199254740992 })));
  assert.throws(() => normalize(sample({ reasoningOutputTokens: 21 })));
  assert.throws(() => normalize(sample({ cachedInputTokens: 101 })));
  assert.equal(estimate(normalize(sample({ cachedInputTokens: null }))).costUsd, null);
  assert.equal(estimate(normalize(sample({ model: 'not-verified' }))).costUsd, null);
  assert.equal(estimate(normalize(sample({ cacheWriteDuration: '12h' }))).costUsd, '0.00179');
  assert.equal(
    estimate(
      normalize(
        sample({
          inputTokens: '272001',
          cachedInputTokens: '0',
          cacheWriteInputTokens: '0',
          outputTokens: '2',
          reasoningOutputTokens: '0',
        }),
      ),
    ).costUsd,
    '5.44017',
  );
});
test('deduplication and conflicts persist without changing accepted event', () => {
  const db = openDb(':memory:', epoch);
  assert.equal(insertEvent(db, sample()), 'inserted');
  assert.equal(insertEvent(db, sample()), 'duplicate');
  assert.equal(insertEvent(db, sample({ outputTokens: '21' })), 'conflict');
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 1);
  assert.equal(db.prepare('SELECT count(*) n FROM errors').get().n, 1);
  db.close();
});
test('analytics includes >100 events and shares all filters with pagination', () => {
  const db = openDb(':memory:', epoch);
  for (let i = 0; i < 155; i++)
    insertEvent(
      db,
      sample({
        responseId: String(i),
        agent: i % 2 ? 'Codex' : 'Other',
        model: i % 2 ? 'gpt-6-astra' : 'gpt-6.1-sol',
        occurredAt: new Date(epoch - i * 1000).toISOString(),
      }),
    );
  const f = filters({ window: '1h' }, epoch),
    a = analytics(db, f);
  assert.equal(a.totals.count, 155);
  assert.equal(a.totals.totalTokens, '18600');
  assert.equal(events(db, f, { limit: 100 }).events.length, 100);
  assert.equal(
    a.buckets.reduce((n, b) => n + b.count, 0),
    155,
  );
  const filtered = filters({ window: '15m', agent: 'Codex', model: 'gpt-6-astra' }, epoch);
  assert.equal(analytics(db, filtered).totals.count, 77);
  assert.equal(events(db, filtered).total, 77);
  assert.equal(events(db, filtered, { offset: 50 }).events.length, 27);
  assert.throws(() => filters({ window: 'bad' }, epoch));
  assert.throws(() => events(db, f, { limit: 101 }));
  db.close();
});
test('collector excludes past usage, reads partial writes, restores model and cursor, rotates and detects conflicts', () => {
  const dir = mkdtempSync(join(tmpdir(), 'usage-monitor-collector-')),
    log = join(dir, 'log.jsonl'),
    dbPath = join(dir, 'usage.sqlite');
  let db = openDb(dbPath, epoch);
  const context = (turn, model) =>
    JSON.stringify({ type: 'turn_context', payload: { turn_id: turn, model } }) + '\n';
  const usage = (id, time = epoch, turn = 't', input = 100) =>
    JSON.stringify({
      type: 'token_usage_record',
      timestamp: new Date(time).toISOString(),
      payload: {
        response_id: id,
        turn_id: turn,
        usage: {
          input_tokens: input,
          output_tokens: 20,
          cached_input_tokens: 40,
          cache_write_input_tokens: 10,
          reasoning_output_tokens: 5,
        },
        turn_token_usage: { input_tokens: 99999999 },
      },
    }) + '\n';
  writeFileSync(log, context('t', 'gpt-6-astra') + usage('past', epoch - 1) + usage('new'));
  let collector = new Collector(db, dir);
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 1);
  const partial = usage('partial');
  appendFileSync(log, partial.slice(0, 50));
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 1);
  db.close();
  db = openDb(dbPath, epoch + 9999);
  collector = new Collector(db, dir);
  appendFileSync(log, partial.slice(50));
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 2);
  appendFileSync(log, context('t2', 'gpt-6.1-sol') + usage('modelchange', epoch, 't2'));
  collector.scan();
  assert.equal(db.prepare('SELECT model FROM events ORDER BY id DESC').get().model, 'gpt-6.1-sol');
  renameSync(log, log + '.1');
  writeFileSync(log, context('t', 'gpt-6-astra') + usage('new') + usage('rotated'));
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 4);
  appendFileSync(log, usage('new', epoch, 't', 102));
  collector.scan();
  assert.equal(
    db.prepare("SELECT count(*) n FROM errors WHERE kind='response_conflict'").get().n,
    1,
  );
  writeFileSync(log, context('t', 'gpt-6-astra') + usage('truncated'));
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 5);
  writeFileSync(join(dir, 'fresh.jsonl'), context('x', 'gpt-6-astra') + usage('fresh', epoch, 'x'));
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 6);
  appendFileSync(
    log,
    JSON.stringify({ type: 'token_count', payload: { input_tokens: 1000 } }) + '\n',
  );
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 6);
  db.close();
  rmSync(dir, { recursive: true });
});
test('tokens and sessions expire at exactly eight hours from issuance; issue revokes previous access', () => {
  const db = openDb(':memory:', epoch);
  const a = issue(db, epoch);
  assert.equal(Date.parse(a.expiresAt), epoch + 8 * 3600000);
  const s = exchange(db, a.token, epoch + 7 * 3600000);
  assert.equal(s.expiresAt, a.expiresAt);
  assert.equal(exchange(db, a.token, epoch), null);
  const b = issue(db, epoch);
  assert.equal(db.prepare('SELECT count(*) n FROM sessions').get().n, 0);
  assert.equal(exchange(db, b.token, epoch + 8 * 3600000), null);
  revoke(db);
  assert.equal(db.prepare('SELECT count(*) n FROM accesses').get().n, 0);
  db.close();
});
test('Antigravity rejects unverified schemas and never supplies a fixed model', () => {
  assert.throws(() => antigravity(sample()));
  assert.throws(() =>
    antigravity({
      ...sample(),
      model: null,
      schema: 'usage-monitor.usage.v1',
      semantics: 'input_includes_cache;output_includes_reasoning',
    }),
  );
  assert.equal(
    antigravity({
      ...sample(),
      model: 'verified-model',
      schema: 'usage-monitor.usage.v1',
      semantics: 'input_includes_cache;output_includes_reasoning',
    }).model,
    'verified-model',
  );
});
test('HTTP access, atomic double exchange, origin/CSRF, security headers and independent local ingest', async () => {
  const db = openDb(':memory:', epoch);
  let now = epoch;
  const c = {
    db,
    origin: 'https://usage.test',
    gatewayKey: 'gateway',
    ingestKey: 'ingest',
    collector: { status: { state: 'running' } },
    now: () => now,
  };
  const { app, close } = createApp(c);
  const web = (method, path) =>
    request(app)[method](path).set('x-usage-monitor-gateway', 'gateway');
  assert.equal((await request(app).get('/api/events')).status, 403);
  for (const path of ['/api/events', '/api/analytics', '/api/stream', '/assets/private.js'])
    assert.equal((await web('get', path)).status, 401);
  const denied = await web('get', '/api/events');
  assert.equal(denied.headers['cache-control'], 'no-store');
  assert.equal(denied.headers['x-frame-options'], 'DENY');
  assert.ok(denied.headers['content-security-policy'].includes("frame-ancestors 'none'"));
  const token = issue(db, epoch).token;
  assert.equal((await web('post', '/auth/exchange').send({ token })).status, 403);
  assert.equal(
    (await web('post', '/auth/exchange').set('origin', c.origin).send({ token: 'bad' })).status,
    401,
  );
  const rr = await Promise.all(
    [1, 2].map(() => web('post', '/auth/exchange').set('origin', c.origin).send({ token })),
  );
  assert.deepEqual(rr.map((r) => r.status).sort(), [200, 401]);
  const r = rr.find((r) => r.status === 200),
    cookie = r.headers['set-cookie'][0].split(';')[0];
  assert.ok(cookie.startsWith(cookieName + '='));
  for (const attr of ['Secure', 'HttpOnly', 'SameSite=Strict', 'Path=/'])
    assert.ok(r.headers['set-cookie'][0].includes(attr));
  assert.equal((await web('get', '/api/analytics').set('cookie', cookie)).status, 200);
  assert.equal((await web('post', '/api/ingest').set('cookie', cookie).send(sample())).status, 403);
  assert.equal(
    (await request(app).post('/api/ingest').set('authorization', 'Bearer ingest').send(sample()))
      .status,
    200,
  );
  assert.equal(
    (await web('post', '/auth/logout').set('cookie', cookie).set('origin', c.origin).send({}))
      .status,
    403,
  );
  assert.equal(
    (
      await web('post', '/auth/logout')
        .set('cookie', cookie)
        .set('origin', c.origin)
        .set('x-csrf-token', r.body.csrf)
        .send({})
    ).status,
    200,
  );
  assert.equal((await web('get', '/api/events').set('cookie', cookie)).status, 401);
  const next = issue(db, epoch);
  const login = await web('post', '/auth/exchange')
    .set('origin', c.origin)
    .send({ token: next.token });
  now = epoch + 8 * 3600000;
  assert.equal(
    (await web('get', '/auth/session').set('cookie', login.headers['set-cookie'][0].split(';')[0]))
      .status,
    401,
  );
  close();
  db.close();
});
test('exchange attempts are rate limited', async () => {
  const db = openDb(':memory:');
  const { app, close } = createApp({
    db,
    origin: 'https://usage.test',
    gatewayKey: 'g',
    ingestKey: 'i',
    collector: { status: {} },
  });
  for (let i = 0; i < 20; i++)
    assert.equal(
      (
        await request(app)
          .post('/auth/exchange')
          .set('x-usage-monitor-gateway', 'g')
          .set('origin', 'https://usage.test')
          .send({ token: 'bad' })
      ).status,
      401,
    );
  assert.equal(
    (
      await request(app)
        .post('/auth/exchange')
        .set('x-usage-monitor-gateway', 'g')
        .set('origin', 'https://usage.test')
        .send({ token: 'bad' })
    ).status,
    429,
  );
  close();
  db.close();
});
test('SSE closes on revocation and on expiry', async () => {
  const db = openDb(':memory:', epoch);
  let now = epoch;
  const { app, close } = createApp({
    db,
    origin: 'https://usage.test',
    gatewayKey: 'g',
    ingestKey: 'i',
    collector: { status: {} },
    now: () => now,
  });
  const server = app.listen(0, '127.0.0.1');
  await new Promise((r) => server.on('listening', r));
  for (const reason of ['revoke', 'expire']) {
    now = epoch;
    const a = issue(db, now),
      s = exchange(db, a.token, now);
    const r = await fetch(`http://127.0.0.1:${server.address().port}/api/stream`, {
      headers: { 'x-usage-monitor-gateway': 'g', cookie: cookieName + '=' + s.session },
    });
    const reader = r.body.getReader();
    assert.match(new TextDecoder().decode((await reader.read()).value), /ready/);
    if (reason === 'revoke') revoke(db);
    else now = epoch + 8 * 3600000;
    const chunk = await Promise.race([
      reader.read(),
      new Promise((_, rej) => setTimeout(() => rej(Error('SSE timeout')), 2000).unref()),
    ]);
    assert.match(new TextDecoder().decode(chunk.value), /expired/);
    assert.equal((await reader.read()).done, true);
  }
  close();
  await new Promise((r) => server.close(r));
  db.close();
});
test('collector preserves huge JSON integers and detects same-size rewritten files', () => {
  const dir = mkdtempSync(join(tmpdir(), 'usage-monitor-large-')),
    db = openDb(':memory:', epoch),
    path = join(dir, 'usage.jsonl'),
    collector = new Collector(db, dir);
  const context =
    JSON.stringify({ type: 'turn_context', payload: { turn_id: 't', model: 'gpt-6-astra' } }) +
    '\n';
  const body = (id) =>
    `{"type":"token_usage_record","timestamp":"2026-10-29T06:00:00Z","payload":{"response_id":"${id}","turn_id":"t","usage":{"input_tokens":9007199254740993123456789,"output_tokens":20,"cached_input_tokens":0,"cache_write_input_tokens":0,"reasoning_output_tokens":0}}}\n`;
  writeFileSync(path, context + body('big-a'));
  collector.scan();
  assert.equal(
    JSON.parse(db.prepare('SELECT data FROM events').get().data).inputTokens,
    '9007199254740993123456789',
  );
  writeFileSync(path, context + body('big-b'));
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 2);
  db.close();
  rmSync(dir, { recursive: true });
});
test('external document entry is allowed without clearing a withheld Strict cookie; cross-site APIs stay blocked', async () => {
  const db = openDb(':memory:');
  const { app, close } = createApp({
    db,
    origin: 'https://usage.test',
    gatewayKey: 'g',
    ingestKey: 'i',
    collector: { status: {} },
    dist: process.cwd() + '/public',
  });
  const entry = (path) =>
    request(app)
      .get(path)
      .set('x-usage-monitor-gateway', 'g')
      .set('sec-fetch-site', 'cross-site')
      .set('sec-fetch-mode', 'navigate')
      .set('sec-fetch-dest', 'document');
  try {
    const root = await entry('/');
    assert.equal(root.status, 303);
    assert.equal(root.headers.location, '/access');
    assert.equal(root.headers['set-cookie'], undefined);
    assert.equal((await entry('/access')).status, 200);
    for (const path of ['/api/events', '/api/analytics', '/api/stream', '/auth/session'])
      assert.equal((await entry(path)).status, 403);
    for (const dest of ['iframe', 'empty'])
      assert.equal(
        (
          await request(app)
            .get('/access')
            .set('x-usage-monitor-gateway', 'g')
            .set('sec-fetch-site', 'cross-site')
            .set('sec-fetch-mode', 'navigate')
            .set('sec-fetch-dest', dest)
        ).status,
        403,
      );
    assert.equal(
      (
        await request(app)
          .get('/access')
          .set('x-usage-monitor-gateway', 'g')
          .set('sec-fetch-site', 'cross-site')
          .set('sec-fetch-mode', 'cors')
          .set('sec-fetch-dest', 'document')
      ).status,
      403,
    );
    const token = issue(db).token;
    assert.equal(
      (
        await request(app)
          .post('/auth/exchange')
          .set('x-usage-monitor-gateway', 'g')
          .set('origin', 'https://usage.test')
          .set('sec-fetch-site', 'cross-site')
          .set('sec-fetch-mode', 'navigate')
          .set('sec-fetch-dest', 'document')
          .send({ token })
      ).status,
      403,
    );
    assert.equal(db.prepare('SELECT used FROM accesses').get().used, 0);
  } finally {
    close();
    db.close();
  }
});
