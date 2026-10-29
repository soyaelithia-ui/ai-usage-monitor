import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync, statSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { execFileSync, spawnSync } from 'node:child_process';
import { openDb } from '../server/db.js';
import { Collector } from '../server/collector.js';
const setup = new URL('../ops/setup.mjs', import.meta.url).pathname;
test('setup generates independent private credentials outside source and preserves existing access configuration', () => {
  const root = mkdtempSync(join(tmpdir(), 'usage-config-')),
    configDir = join(root, 'config'),
    dataDir = join(root, 'data');
  try {
    const args = [setup, '--config-dir', configDir, '--data-dir', dataDir],
      output = execFileSync(process.execPath, args, { encoding: 'utf8' });
    const original = readFileSync(join(configDir, 'backend.json'), 'utf8'),
      backend = JSON.parse(original),
      gateway = JSON.parse(readFileSync(join(configDir, 'gateway.json'), 'utf8'));
    assert.match(backend.gatewayKey, /^[A-Za-z0-9_-]{43}$/);
    assert.match(backend.ingestKey, /^[A-Za-z0-9_-]{43}$/);
    assert.notEqual(backend.gatewayKey, backend.ingestKey);
    assert.equal(gateway.gatewayKey, backend.gatewayKey);
    assert.equal(gateway.ingestKey, undefined);
    assert.equal(statSync(join(configDir, 'backend.json')).mode & 0o777, 0o600);
    assert.equal(statSync(configDir).mode & 0o777, 0o700);
    assert.equal(backend.codexRoot, null);
    assert.equal(backend.dataDir, dataDir);
    assert.ok(!output.includes(backend.gatewayKey) && !output.includes(backend.ingestKey));
    const again = spawnSync(process.execPath, args, { encoding: 'utf8' });
    assert.notEqual(again.status, 0);
    assert.equal(readFileSync(join(configDir, 'backend.json'), 'utf8'), original);
  } finally {
    rmSync(root, { recursive: true });
  }
});
test('configuration rejects credential reuse, remote plaintext origins and invalid proxy configuration', () => {
  const root = mkdtempSync(join(tmpdir(), 'usage-invalid-')),
    path = join(root, 'backend.json');
  const module = new URL('../server/config.js', import.meta.url).href;
  try {
    const init = spawnSync(
      process.execPath,
      [setup, '--config-dir', join(root, 'cfg'), '--data-dir', join(root, 'data')],
      { encoding: 'utf8' },
    );
    assert.equal(init.status, 0);
    const good = JSON.parse(readFileSync(join(root, 'cfg', 'backend.json'), 'utf8'));
    const check = (value) => {
      writeFileSync(path, JSON.stringify(value));
      return spawnSync(
        process.execPath,
        ['--input-type=module', '-e', `import {config} from ${JSON.stringify(module)};config();`],
        { env: { ...process.env, USAGE_MONITOR_CONFIG: path }, encoding: 'utf8' },
      ).status;
    };
    assert.equal(check(good), 0);
    assert.notEqual(check({ ...good, ingestKey: good.gatewayKey }), 0);
    assert.notEqual(check({ ...good, origin: 'http://usage.example.com' }), 0);
    assert.notEqual(check({ ...good, origin: 'https://user:password@usage.example.com' }), 0);
    assert.notEqual(check({ ...good, gatewayPort: good.backendPort }), 0);
  } finally {
    rmSync(root, { recursive: true });
  }
});
test('unconfigured collector waits without reading source paths or recording failures', () => {
  const db = openDb(':memory:');
  const collector = new Collector(db, null);
  collector.start();
  assert.equal(collector.status.state, 'waiting');
  assert.equal(db.prepare('SELECT count(*) n FROM errors').get().n, 0);
  collector.stop();
  db.close();
});
