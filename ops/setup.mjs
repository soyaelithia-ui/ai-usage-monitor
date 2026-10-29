import { mkdirSync, existsSync, writeFileSync, chmodSync } from 'node:fs';
import { join, resolve, dirname } from 'node:path';
import { homedir } from 'node:os';
import { randomBytes } from 'node:crypto';
import { parseArgs } from 'node:util';
const { values } = parseArgs({
  options: {
    origin: { type: 'string', default: 'http://localhost:4318' },
    'config-dir': { type: 'string' },
    'data-dir': { type: 'string' },
  },
});
let origin;
try {
  origin = new URL(values.origin);
} catch {
  throw Error('Invalid origin');
}
if (
  origin.username ||
  origin.password ||
  origin.search ||
  origin.hash ||
  origin.pathname !== '/' ||
  !(
    origin.protocol === 'https:' ||
    (origin.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(origin.hostname))
  )
)
  throw Error('Use HTTPS or loopback HTTP');
const configDir = resolve(
  values['config-dir'] ??
    join(process.env.XDG_CONFIG_HOME ?? join(homedir(), '.config'), 'ai-usage-monitor'),
);
const dataDir = resolve(
  values['data-dir'] ??
    join(process.env.XDG_DATA_HOME ?? join(homedir(), '.local', 'share'), 'ai-usage-monitor'),
);
const backend = join(configDir, 'backend.json'),
  gateway = join(configDir, 'gateway.json');
if (existsSync(backend) || existsSync(gateway))
  throw Error('Configuration already exists; existing credentials were preserved.');
process.umask(0o077);
for (const p of [configDir, dataDir]) {
  mkdirSync(p, { recursive: true, mode: 0o700 });
  chmodSync(p, 0o700);
}
const gatewayKey = randomBytes(32).toString('base64url'),
  ingestKey = randomBytes(32).toString('base64url');
const shared = { origin: origin.origin, backendPort: 4317, gatewayPort: 4318, gatewayKey };
writeFileSync(
  backend,
  JSON.stringify(
    {
      ...shared,
      ingestKey,
      dataDir,
      db: join(dataDir, 'usage.sqlite'),
      tokenizerSocket: join(dataDir, 'tokenizer.sock'),
      codexRoot: null,
      openCodeDb: null,
      geminiRoot: null,
      antigravityRoot: null,
    },
    null,
    2,
  ) + '\n',
  { mode: 0o600, flag: 'wx' },
);
writeFileSync(gateway, JSON.stringify(shared, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
console.log('Configuration created with independent credentials.');
console.log('Backend configuration:', backend);
console.log('Gateway configuration:', gateway);
