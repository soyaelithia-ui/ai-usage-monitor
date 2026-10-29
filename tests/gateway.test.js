import test from 'node:test';
import assert from 'node:assert/strict';
import { gateway } from '../server/gateway.js';
import { createServer, request } from 'node:http';
const listen = (s) => new Promise((r) => s.listen(0, '127.0.0.1', () => r(s.address().port)));
test('gateway validates tunnel headers, strips credentials and disallows ingestion', async () => {
  let headers;
  const backend = createServer((req, res) => {
    headers = req.headers;
    res.end('ok');
  });
  const backendPort = await listen(backend);
  const c = {
    origin: 'https://usage.example',
    backendPort,
    gatewayPort: 0,
    gatewayKey: 'private-key',
  };
  const proxy = gateway(c);
  c.gatewayPort = await listen(proxy);
  const call = (path, h = {}) =>
    new Promise((resolve, reject) => {
      const r = request({ host: '127.0.0.1', port: c.gatewayPort, path, headers: h }, (r) => {
        r.resume();
        r.on('end', () => resolve(r.statusCode));
      });
      r.on('error', reject);
      r.end();
    });
  const valid = {
    host: `127.0.0.1:${c.gatewayPort}`,
    'x-forwarded-host': 'usage.example',
    'x-forwarded-proto': 'https',
  };
  assert.equal(await call('/api/events'), 421);
  assert.equal(
    await call('/api/events', { ...valid, 'x-forwarded-host': 'attacker.example' }),
    421,
  );
  assert.equal(await call('/api/events', { ...valid, 'x-forwarded-proto': 'http' }), 421);
  assert.equal(
    await call('/api/events', { ...valid, 'x-forwarded-host': 'usage.example, attacker.example' }),
    421,
  );
  assert.equal(
    await call('/api/events', {
      ...valid,
      authorization: 'Bearer malicious',
      'x-usage-monitor-gateway': 'malicious',
      'x-forwarded-for': '1.2.3.4',
    }),
    200,
  );
  assert.equal(headers.authorization, undefined);
  assert.equal(headers['x-usage-monitor-gateway'], c.gatewayKey);
  assert.equal(headers['x-forwarded-for'], undefined);
  assert.equal(
    await call('/access', {
      ...valid,
      'sec-fetch-site': 'cross-site',
      'sec-fetch-mode': 'navigate',
      'sec-fetch-dest': 'document',
    }),
    200,
  );
  assert.equal(headers['sec-fetch-site'], 'cross-site');
  assert.equal(headers['sec-fetch-mode'], 'navigate');
  assert.equal(headers['sec-fetch-dest'], 'document');
  for (const path of [
    '/api/ingest',
    '/server/config.js',
    '/.env',
    '/node_modules/x',
    '/auth/../api/ingest',
  ])
    assert.equal(await call(path, valid), 404);
  await Promise.all([new Promise((r) => proxy.close(r)), new Promise((r) => backend.close(r))]);
});
