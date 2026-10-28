import express, { type ErrorRequestHandler, type Response } from 'express';
import { join } from 'node:path';
import { existsSync } from 'node:fs';
import { z } from 'zod';
import type { PricingCatalog } from '../shared/types.js';
import { ConflictError, UsageStore } from './store.js';

const usageSchema = z.object({
  agent_name: z.string().trim().min(1).max(160),
  model: z.string().trim().min(1).max(160),
  prompt_tokens: z.number().int().nonnegative().safe(),
  completion_tokens: z.number().int().nonnegative().safe(),
  request_id: z.string().trim().min(1).max(200).optional(),
}).strict();
const querySchema = z.object({
  before_id: z.coerce.number().int().positive().safe().optional(),
  limit: z.coerce.number().int().min(1).max(100).default(100),
}).strict();

export function createApp(options: {
  store: UsageStore; catalog: PricingCatalog; staticDir?: string; now?: () => Date;
}) {
  const { store, catalog, staticDir } = options;
  const now = options.now ?? (() => new Date());
  const app = express();
  const clients = new Set<Response>();
  app.disable('x-powered-by');
  app.use((req, res, next) => {
    res.set('X-Content-Type-Options', 'nosniff');
    res.set('Referrer-Policy', 'no-referrer');
    const allowed = new Set(['localhost', '127.0.0.1', '[::1]']);
    try {
      if (!allowed.has(new URL(`http://${req.headers.host}`).hostname)) return res.status(403).json({ error: 'Acceso local requerido' });
      if (req.headers.origin && !allowed.has(new URL(req.headers.origin).hostname)) return res.status(403).json({ error: 'Origen no permitido' });
    } catch { return res.status(403).json({ error: 'Origen inválido' }); }
    next();
  });
  app.use('/api', (_req, res, next) => { res.set('Cache-Control', 'no-store'); next(); });
  app.use(express.json({ limit: '16kb' }));

  const send = (client: Response, type: string, payload: unknown, id?: number) => {
    if (client.destroyed) { clients.delete(client); return; }
    const data = `${id === undefined ? '' : `id: ${id}\n`}event: ${type}\ndata: ${JSON.stringify(payload)}\n\n`;
    // A slow tab reconnects to a fresh snapshot instead of retaining an unbounded queue.
    if (!client.write(data) && client.writableLength > 1_048_576) client.destroy();
  };

  app.post('/api/usage', (req, res) => {
    if (!req.is('application/json')) return res.status(415).json({ error: 'Usa Content-Type: application/json' });
    const parsed = usageSchema.safeParse(req.body);
    if (!parsed.success) return res.status(400).json({ error: 'Métricas inválidas', details: parsed.error.flatten() });
    try {
      const { event, duplicate } = store.ingest(parsed.data, now().toISOString());
      if (!duplicate) {
        const update = { event, ...store.summary() };
        for (const client of clients) send(client, 'usage', update, event.id);
      }
      return res.status(duplicate ? 200 : 201).json({ event, duplicate });
    } catch (error) {
      if (error instanceof ConflictError) return res.status(409).json({ error: error.message });
      throw error;
    }
  });

  app.get('/api/stream', (req, res) => {
    res.set({ 'Content-Type': 'text/event-stream; charset=utf-8', 'Cache-Control': 'no-cache, no-transform',
      Connection: 'keep-alive', 'X-Accel-Buffering': 'no' });
    res.flushHeaders();
    res.write('retry: 1500\n\n');
    // SQLite operations and subscription are synchronous: no ingest can interleave here.
    clients.add(res);
    const snapshot = store.snapshot();
    send(res, 'snapshot', snapshot, snapshot.last_event_id);
    const heartbeat = setInterval(() => {
      if (!res.destroyed) res.write(': heartbeat\n\n');
    }, 15_000);
    req.on('close', () => { clearInterval(heartbeat); clients.delete(res); });
  });

  app.get('/api/events', (req, res) => {
    const parsed = querySchema.safeParse(req.query);
    if (!parsed.success) return res.status(400).json({ error: 'Paginación inválida' });
    return res.json(store.events(parsed.data.before_id, parsed.data.limit));
  });
  app.get('/api/pricing', (_req, res) => res.json({ ...catalog, server_date: now().toISOString().slice(0, 10) }));
  app.get('/api/health', (_req, res) => { store.health(); res.json({ status: 'ok', storage: 'sqlite' }); });
  app.use('/api', (_req, res) => res.status(404).json({ error: 'Endpoint no encontrado' }));
  if (staticDir && existsSync(join(staticDir, 'index.html'))) {
    app.use(express.static(staticDir));
    app.get('/', (_req, res) => res.sendFile(join(staticDir, 'index.html')));
  } else {
    app.get('/', (_req, res) => res.type('text').send('Pulse API activa. Abre http://127.0.0.1:5173 durante el desarrollo.'));
  }
  const handleError: ErrorRequestHandler = (error, _req, res, _next) => {
    const status = error.type === 'entity.too.large' ? 413 : error.type === 'entity.parse.failed' ? 400 : 500;
    if (status === 500) console.error('Error del servicio:', error.message);
    if (!res.headersSent) res.status(status).json({ error: status === 500 ? 'No se pudo completar la operación' : 'Cuerpo JSON inválido o demasiado grande' });
    else res.end();
  };
  app.use(handleError);
  return { app, closeStreams: () => { for (const client of clients) client.end(); clients.clear(); } };
}
