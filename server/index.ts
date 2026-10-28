import { resolve } from 'node:path';
import { loadPricing } from './pricing.js';
import { UsageStore } from './store.js';
import { createApp } from './app.js';

const port = Number(process.env.PORT ?? 3000);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('PORT inválido');
const catalog = loadPricing(resolve(process.env.PRICING_PATH ?? 'config/pricing.json'));
const store = new UsageStore(resolve(process.env.DB_PATH ?? 'data/usage.sqlite'), catalog);
const { app, closeStreams } = createApp({ store, catalog, staticDir: resolve('dist/client') });
const server = app.listen(port, '127.0.0.1', () => {
  console.log(`Pulse · http://127.0.0.1:${port} · ${Object.keys(catalog.models).length} modelos configurados`);
});
server.on('error', error => { console.error(error.message); store.close(); process.exitCode = 1; });
let stopping = false;
function stop() {
  if (stopping) return;
  stopping = true;
  closeStreams();
  server.close(() => { store.close(); process.exit(0); });
  setTimeout(() => { server.closeAllConnections(); }, 2000).unref();
}
process.on('SIGINT', stop);
process.on('SIGTERM', stop);
