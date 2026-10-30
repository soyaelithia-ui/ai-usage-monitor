import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, statSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import Database from 'better-sqlite3';
import request from 'supertest';
import { openCodeMessage, visibleText } from '../server/adapters.js';
import { Sources } from '../server/sources.js';
import { normalize } from '../server/usage.js';
import {
  openDb,
  insertEvent,
  analytics,
  events,
  filters,
  hash,
  issue,
  exchange,
} from '../server/db.js';
import { reconcile } from '../server/provenance.js';
import { createApp, cookieName } from '../server/app.js';
const epoch = Date.parse('2026-10-29T06:00:00Z');
const oc = (extra = {}) => ({
  id: 'message-1',
  type: 'assistant',
  model: { id: 'gpt-6-astra', providerID: 'openai' },
  time: { created: epoch, completed: epoch + 1 },
  tokens: { input: 10, output: 20, reasoning: 30, cache: { read: 40, write: 50 } },
  ...extra,
});
const setup = () => {
  const dir = mkdtempSync(join(tmpdir(), 'usage-monitor-native-'));
  return { dir, db: openDb(':memory:', epoch) };
};
const openCodeFixture = (dir) => {
  const path = join(dir, 'opencode.db');
  const source = new Database(path);
  source.exec(
    'CREATE TABLE session_v2(id TEXT PRIMARY KEY,version TEXT); CREATE TABLE session_message(id TEXT PRIMARY KEY,session_id TEXT,type TEXT,time_updated INTEGER,data TEXT);',
  );
  source.prepare('INSERT INTO session_v2 VALUES (?,?)').run('s', '2.1.4');
  const put = (m, at) =>
    source
      .prepare(
        'INSERT INTO session_message VALUES (?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET time_updated=excluded.time_updated,data=excluded.data',
      )
      .run(m.id, 's', m.type, at, JSON.stringify(m));
  return { path, source, put };
};
test('synthetic usage fixture preserves exact totals and decimal components', () => {
  const db = openDb(':memory:', epoch);
  const rows = JSON.parse(readFileSync(new URL('./fixtures/baseline.json', import.meta.url)));
  for (const row of rows) insertEvent(db, row);
  const a = analytics(db, filters({ window: '7d' }, Date.parse('2026-01-02T00:00:00Z'))).totals;
  assert.equal(a.count, 3);
  assert.equal(a.inputTokens, '103000');
  assert.equal(a.outputTokens, '10500');
  assert.equal(a.cachedInputTokens, '76000');
  assert.equal(a.normalInputTokens, '22000');
  assert.equal(a.totalTokens, '113500');
  assert.equal(a.costUsd, '0.8835');
  assert.equal(a.costInputUsd, '0.22');
  assert.equal(a.costCacheUsd, '0.1385');
  assert.equal(a.costOutputUsd, '0.525');
  db.close();
});
test('OpenCode 2.1.4 semantics, complete-only and precision; unknown schemas rejected', () => {
  const e = normalize(openCodeMessage(oc(), 's', '2.1.4'));
  assert.equal(e.inputTokens, '100');
  assert.equal(e.outputTokens, '50');
  assert.equal(e.totalTokens, '150');
  assert.equal(openCodeMessage(oc({ time: { created: epoch } }), 's', '2.1.4'), null);
  assert.throws(() => openCodeMessage(oc(), 's', '1.0.0'));
  assert.throws(() => openCodeMessage(oc({ model: {} }), 's', '2.1.4'));
  assert.throws(() => openCodeMessage(oc({ tokens: { input: 1 } }), 's', '2.1.4'));
  const comp = oc({ type: 'compaction', status: 'completed' });
  assert.equal(openCodeMessage(comp, 's', '2.1.4').inputTokens, '100');
  assert.equal(openCodeMessage({ ...comp, status: 'running' }, 's', '2.1.4'), null);
});
test('new metadata enriches a duplicate without repricing, mutation or invalidating access', () => {
  const db = openDb(':memory:', epoch),
    input = openCodeMessage(oc(), 's', '2.1.4');
  const old = normalize(input);
  insertEvent(db, old);
  const price = JSON.parse(db.prepare('SELECT data FROM events').get().data).costUsd;
  const access = issue(db, epoch);
  const session = exchange(db, access.token, epoch);
  assert.equal(insertEvent(db, input), 'duplicate');
  assert.equal(JSON.parse(db.prepare('SELECT data FROM events').get().data).costUsd, price);
  assert.equal(db.prepare('SELECT count(*) n FROM sessions').get().n, 1);
  assert.equal(
    db.prepare('SELECT expires FROM sessions WHERE hash=?').get(hash(session.session)).expires,
    epoch + 8 * 3600000,
  );
  const f = filters({ window: '1h' }, epoch + 10);
  assert.equal(reconcile(db, f).matched, 1);
  assert.equal(insertEvent(db, { ...input, inputTokens: '101' }), 'conflict');
  assert.equal(reconcile(db, f).conflicts, 1);
  insertEvent(db, input);
  assert.equal(reconcile(db, f).conflicts, 1);
  db.close();
});
test('more than 100 events share provider/session/method filters and reconciliation', () => {
  const db = openDb(':memory:', epoch);
  for (let i = 0; i < 180; i++)
    insertEvent(db, openCodeMessage(oc({ id: String(i) }), i % 2 ? 's1' : 's2', '2.1.4'));
  const f = filters(
    { window: '1h', provider: 'openai', sessionId: 's1', method: 'native' },
    epoch + 100,
  );
  assert.equal(analytics(db, f).totals.count, 90);
  assert.equal(events(db, f).total, 90);
  assert.equal(reconcile(db, f).matched, 90);
  assert.equal(reconcile(db, f).sessions.length, 1);
  assert.equal(analytics(db, f).options.providers[0], 'openai');
  db.close();
});
test('read-only OpenCode collector tracks finalization, restart, old cutoff, conflicts and large batches', () => {
  const { dir, db } = setup(),
    { path, source, put } = openCodeFixture(dir);
  put(oc({ time: { created: epoch } }), epoch);
  put(oc({ id: 'past', time: { created: epoch - 50, completed: epoch - 1 } }), epoch);
  let collector = new Sources(db, { openCodeDb: path });
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 0);
  put(oc(), epoch + 1);
  const before = hash(readFileSync(path)),
    mtime = statSync(path).mtimeMs;
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 1);
  assert.equal(hash(readFileSync(path)), before);
  assert.equal(statSync(path).mtimeMs, mtime);
  collector = new Sources(db, { openCodeDb: path });
  collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 1);
  for (let i = 0; i < 420; i++) put(oc({ id: 'batch-' + String(i).padStart(4, '0') }), epoch + 2);
  for (let i = 0; i < 5; i++) collector.scan();
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 421);
  put(oc({ tokens: { ...oc().tokens, input: 11 } }), epoch + 3);
  collector.scan();
  assert.equal(
    db.prepare("SELECT count(*) n FROM errors WHERE kind='response_conflict'").get().n,
    1,
  );
  source.close();
  db.close();
  rmSync(dir, { recursive: true });
});
test('fallback only extracts explicit visible text, rejects tools/media and does not alter native totals', () => {
  assert.equal(
    visibleText([
      { type: 'text', text: 'a' },
      { type: 'text', text: 'b' },
    ]),
    'ab',
  );
  assert.equal(visibleText([{ type: 'reasoning', text: 'private' }]), null);
  assert.equal(visibleText([{ inlineData: {} }]), null);
  const { dir, db } = setup();
  let queued;
  const tokenizer = {
    enqueue: (input, text) => {
      queued = { input, text };
    },
  };
  const { path, put } = openCodeFixture(dir);
  put(
    oc({
      tokens: undefined,
      content: [{ type: 'text', text: 'hola 👋' }],
    }),
    epoch + 1,
  );
  const collector = new Sources(db, { openCodeDb: path }, tokenizer);
  collector.scan();
  assert.equal(queued.text, 'hola 👋');
  assert.equal(queued.input.model, 'gpt-6-astra');
  assert.equal(queued.input.agent, 'OpenCode');
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 0);
  db.close();
  rmSync(dir, { recursive: true });
});
test('source and reconciliation routes require authentication and return filtered native provenance', async () => {
  const db = openDb(':memory:', epoch);
  const { app, close } = createApp({
    db,
    origin: 'https://usage.test',
    gatewayKey: 'g',
    ingestKey: 'i',
    collector: { status: { state: 'running' } },
    now: () => epoch,
  });
  const s = exchange(db, issue(db, epoch).token, epoch);
  for (const path of ['/api/sources', '/api/reconciliation']) {
    assert.equal((await request(app).get(path).set('x-usage-monitor-gateway', 'g')).status, 401);
    assert.equal(
      (
        await request(app)
          .get(path)
          .set('x-usage-monitor-gateway', 'g')
          .set('cookie', cookieName + '=' + s.session)
      ).status,
      200,
    );
  }
  close();
  db.close();
});
