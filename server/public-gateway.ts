import { randomBytes } from 'node:crypto';
import { chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { createGateway } from './gateway.js';

const gatewayPort = Number(process.env.PUBLIC_GATEWAY_PORT ?? 3001);
const upstreamPort = Number(process.env.PORT ?? 3000);
if (![gatewayPort, upstreamPort].every(port => Number.isInteger(port) && port > 0 && port <= 65535) || gatewayPort === upstreamPort) {
  throw new Error('PUBLIC_GATEWAY_PORT y PORT deben ser puertos válidos y diferentes');
}
const tokenPath = resolve(process.env.INGEST_TOKEN_PATH ?? 'data/ingest-token');
mkdirSync(dirname(tokenPath), { recursive: true });
if (!existsSync(tokenPath)) {
  try { writeFileSync(tokenPath, `${randomBytes(32).toString('base64url')}\n`, { mode: 0o600, flag: 'wx' }); }
  catch (error) { if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error; }
}
chmodSync(tokenPath, 0o600);
const token = readFileSync(tokenPath, 'utf8').trim();
if (!/^[A-Za-z0-9_-]{43,}$/.test(token)) throw new Error(`Token inválido en ${tokenPath}`);
const server = createGateway({ upstreamPort, token });
server.listen(gatewayPort, '127.0.0.1', () => {
  console.log(`Gateway público · 127.0.0.1:${gatewayPort} → 127.0.0.1:${upstreamPort} · token en ${tokenPath}`);
});
server.on('error', error => { console.error(error.message); process.exitCode = 1; });
let stopping = false;
function stop() {
  if (stopping) return;
  stopping = true;
  server.close(() => process.exit(0));
  setTimeout(() => server.closeAllConnections(), 2000).unref();
}
process.on('SIGINT', stop);
process.on('SIGTERM', stop);
