import test from 'node:test';
import assert from 'node:assert/strict';
import net from 'node:net';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { Tokenizer } from '../server/tokenizer.js';
import { openDb } from '../server/db.js';
test('tokenizer queue is bounded, keeps text transient and never adds estimated text to native totals', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'usage-monitor-worker-')),
    path = join(dir, 'worker.sock'),
    db = openDb(':memory:');
  const server = net.createServer((socket) => {
    socket.on('data', () =>
      setTimeout(
        () =>
          socket.end(
            JSON.stringify({ status: 'estimated', tokens: '3', tokenizer: 'test', version: '1' }) +
              '\n',
          ),
        5,
      ),
    );
  });
  await new Promise((r) => server.listen(path, r));
  const worker = new Tokenizer(db, path);
  const sample = {
    source: 'test',
    responseId: '1',
    occurredAt: new Date().toISOString(),
    provider: 'openai',
    model: 'gpt-4o',
  };
  worker.enqueue(sample, 'NEVER PERSIST THIS TEXT');
  for (let i = 2; i < 40; i++) worker.enqueue({ ...sample, responseId: String(i) }, 'hello');
  assert.ok(worker.queue.length <= 32);
  const timeout = Date.now() + 3000;
  while (worker.running && Date.now() < timeout) await new Promise((r) => setTimeout(r, 20));
  assert.equal(worker.running, false);
  assert.equal(db.prepare('SELECT count(*) n FROM events').get().n, 0);
  const data = JSON.stringify(db.prepare('SELECT data FROM text_measurements').all());
  assert.ok(!data.includes('NEVER PERSIST'));
  assert.ok(data.includes('queue_limit'));
  worker.stop();
  await new Promise((r) => server.close(r));
  db.close();
  rmSync(dir, { recursive: true });
});
test('missing tokenizer fails without throwing or holding native collection', async () => {
  const worker = new Tokenizer(null, '/tmp/usage-monitor-nonexistent-test.sock');
  assert.equal((await worker.request({ model: 'unknown', text: 'test' })).status, 'unavailable');
  worker.stop();
});
