import { test, type TestContext } from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import type { AddressInfo } from 'node:net';
import { request } from 'node:http';
import { createApp } from '../server/app.js';
import { loadPricing } from '../server/pricing.js';
import { UsageStore } from '../server/store.js';

const metrics = { agent_name: 'writer', model: 'gpt-6.1-sol', prompt_tokens: 1000, completion_tokens: 500 };
async function setup(t: TestContext) {
  const catalog = loadPricing('config/pricing.json');
  const store = new UsageStore(':memory:', catalog);
  const { app, closeStreams } = createApp({ store, catalog, now: () => new Date('2026-10-28T12:00:00Z') });
  const server = app.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  t.after(async () => { closeStreams(); server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); store.close(); });
  const post = (body: unknown) => fetch(`${base}/api/usage`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  return { base, post, store };
}

async function stream(base: string, t: TestContext) {
  const abort = new AbortController();
  const response = await fetch(`${base}/api/stream`, { signal: abort.signal });
  assert.match(response.headers.get('content-type')!, /text\/event-stream/);
  const reader = response.body!.getReader();
  t.after(() => { abort.abort(); reader.cancel().catch(() => {}); });
  let buffer = '';
  const decoder = new TextDecoder();
  return async (kind: string) => {
    while (true) {
      const boundary = buffer.indexOf('\n\n');
      if (boundary >= 0) {
        const frame = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2);
        if (frame.includes(`event: ${kind}\n`)) return JSON.parse(frame.split('\n').find(line => line.startsWith('data: '))!.slice(6));
      } else {
        const result = await reader.read();
        if (result.done) throw new Error('SSE finalizado antes del evento');
        buffer += decoder.decode(result.value, { stream: true });
      }
    }
  };
}

test('validación HTTP, idempotencia y errores de cliente', async t => {
  const { base, post, store } = await setup(t);
  assert.equal((await fetch(`${base}/api/health`)).status, 200);
  for (const body of [null, {}, { ...metrics, prompt_tokens: -1 }, { ...metrics, completion_tokens: 1.5 },
    { ...metrics, prompt_tokens: true }, { ...metrics, prompt_tokens: '100' }, { ...metrics, agent_name: '  ' },
    { ...metrics, prompt_tokens: Number.MAX_SAFE_INTEGER + 1 }, { ...metrics, request_id: '' }]) {
    assert.equal((await post(body)).status, 400);
  }
  assert.equal((await fetch(`${base}/api/usage`, { method: 'POST', body: '{}' })).status, 415);
  assert.equal((await fetch(`${base}/api/usage`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{' })).status, 400);
  assert.equal((await post({ ...metrics, request_id: 'request-1' })).status, 201);
  assert.equal((await post({ ...metrics, request_id: 'request-1' })).status, 200);
  assert.equal((await post({ ...metrics, request_id: 'request-1', completion_tokens: 0 })).status, 409);
  assert.equal(store.summary().totals.requests, 1);
  assert.equal((await fetch(`${base}/api/events?limit=101`)).status, 400);
  assert.equal((await fetch(`${base}/api/events?before_id=no`)).status, 400);
  assert.equal((await fetch(`${base}/api/missing`)).status, 404);
  assert.equal((await fetch(`${base}/api/health`, { headers: { Origin: 'https://outside.example' } })).status, 403);
  const foreignHostStatus = await new Promise<number | undefined>((resolve, reject) => {
    const req = request(`${base}/api/health`, { headers: { Host: 'outside.example' } }, response => { response.resume(); resolve(response.statusCode); });
    req.on('error', reject); req.end();
  });
  assert.equal(foreignHostStatus, 403);
});

test('dos clientes SSE reciben el evento y reconectar devuelve una instantánea completa', { timeout: 10000 }, async t => {
  const { base, post } = await setup(t);
  const first = await stream(base, t);
  const second = await stream(base, t);
  assert.equal((await first('snapshot')).totals.requests, 0);
  assert.equal((await second('snapshot')).totals.requests, 0);
  const before = performance.now();
  await post(metrics);
  const [a, b] = await Promise.all([first('usage'), second('usage')]);
  assert.deepEqual(a, b);
  assert.equal(a.totals.cost_usd, '0.007');
  assert.ok(performance.now() - before < 1000, 'Actualización SSE en menos de un segundo');
  await post({ ...metrics, model: 'modelo-personalizado' });
  const reconnected = await stream(base, t);
  const snapshot = await reconnected('snapshot');
  assert.equal(snapshot.totals.requests, 2);
  assert.equal(snapshot.events.length, 2);
  assert.equal(snapshot.events[0].cost_usd, null);
  assert.equal(snapshot.totals.unpriced_requests, 1);
});

test('ingesta concurrente conserva todos los eventos y sus acumulados', async t => {
  const { post, base, store } = await setup(t);
  const replies = await Promise.all(Array.from({ length: 120 }, (_, index) => post({ ...metrics, request_id: `concurrent-${index}`, agent_name: index % 2 ? 'a' : 'b' })));
  assert.ok(replies.every(response => response.status === 201));
  assert.equal(store.summary().totals.requests, 120);
  assert.equal(store.summary().totals.cost_usd, '0.84');
  const page = await (await fetch(`${base}/api/events?limit=100`)).json();
  const tail = await (await fetch(`${base}/api/events?before_id=${page.next_before_id}`)).json();
  assert.equal(page.events.length, 100);
  assert.equal(tail.events.length, 20);
  assert.equal(new Set([...page.events, ...tail.events].map(event => event.id)).size, 120);
  const pricing = await (await fetch(`${base}/api/pricing`)).json();
  assert.equal(Object.keys(pricing.models).length, 6);
});
