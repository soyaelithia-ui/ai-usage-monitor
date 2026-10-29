import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
export function configPath() {
  return (
    process.env.USAGE_MONITOR_CONFIG ??
    join(
      process.env.XDG_CONFIG_HOME ?? join(homedir(), '.config'),
      'ai-usage-monitor',
      'backend.json',
    )
  );
}
export function config() {
  const path = configPath();
  let c;
  try {
    c = JSON.parse(readFileSync(path, 'utf8'));
  } catch {
    throw Error('Configuration unavailable. Run npm run setup or set USAGE_MONITOR_CONFIG.');
  }
  let origin;
  try {
    origin = new URL(c.origin);
  } catch {
    throw Error('Invalid configured origin');
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
    throw Error('Use an HTTPS origin, or HTTP on loopback for development');
  for (const key of ['backendPort', 'gatewayPort'])
    if (!Number.isInteger(c[key]) || c[key] < 1024 || c[key] > 65535)
      throw Error('Invalid configured port');
  if (c.backendPort === c.gatewayPort) throw Error('Backend and gateway ports must differ');
  if (!/^[A-Za-z0-9_-]{43}$/.test(c.gatewayKey ?? '')) throw Error('Invalid gateway credential');
  if (
    'ingestKey' in c &&
    (!/^[A-Za-z0-9_-]{43}$/.test(c.ingestKey ?? '') || c.ingestKey === c.gatewayKey)
  )
    throw Error('Invalid independent ingestion credential');
  c.origin = origin.origin;
  for (const key of [
    'db',
    'dataDir',
    'codexRoot',
    'openCodeDb',
    'geminiRoot',
    'antigravityRoot',
    'tokenizerSocket',
  ])
    if (c[key]) c[key] = resolve(dirname(path), c[key]);
  return c;
}
