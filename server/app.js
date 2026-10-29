import express from 'express';
import { timingSafeEqual } from 'node:crypto';
import { resolve } from 'node:path';
import { hash, exchange, filters, events, analytics, insertEvent } from './db.js';
import { antigravity } from './usage.js';
import { reconcile } from './provenance.js';
export const cookieName = '__Host-usage-monitor';
export const equal = (a, b) =>
  typeof a === 'string' &&
  typeof b === 'string' &&
  Buffer.byteLength(a) === Buffer.byteLength(b) &&
  timingSafeEqual(Buffer.from(a), Buffer.from(b));
export function security(req, res, next) {
  res.set({
    'Cache-Control': 'no-store',
    'Content-Security-Policy':
      "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'",
    'X-Frame-Options': 'DENY',
    'X-Content-Type-Options': 'nosniff',
    'Referrer-Policy': 'no-referrer',
    'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
    'Strict-Transport-Security': 'max-age=31536000',
  });
  next();
}
export function createApp({
  db,
  origin,
  gatewayKey,
  ingestKey,
  collector,
  sources,
  tokenizer,
  dist = resolve('dist'),
  now = Date.now,
}) {
  const app = express();
  app.disable('x-powered-by');
  app.set('trust proxy', false);
  app.use(security);
  const streams = new Set();
  let revision = 0;
  const broadcast = () => {
    revision++;
    for (const s of streams) s.res.write(`event: change\ndata: ${revision}\n\n`);
  };
  const authenticate = (req) => {
    const cookies = (req.headers.cookie ?? '').split(';').map((x) => x.trim());
    const vals = cookies.filter((x) => x.startsWith(cookieName + '='));
    if (vals.length !== 1) return null;
    const token = vals[0].slice(cookieName.length + 1);
    if (!/^[A-Za-z0-9_-]{43}$/.test(token)) return null;
    const s = db
      .prepare('SELECT * FROM sessions WHERE hash=? AND expires>?')
      .get(hash(token), now());
    return s ?? null;
  };
  const sweep = setInterval(() => {
    for (const s of streams) {
      if (!db.prepare('SELECT 1 FROM sessions WHERE hash=? AND expires>?').get(s.hash, now())) {
        s.res.write('event: expired\ndata: {}\n\n');
        s.res.end();
        streams.delete(s);
      }
    }
  }, 500);
  sweep.unref();
  app.use(express.json({ limit: '32kb' }));
  app.post('/api/ingest', (req, res) => {
    if (
      req.headers['x-usage-monitor-gateway'] ||
      req.headers.origin ||
      !equal(req.headers.authorization, `Bearer ${ingestKey}`)
    )
      return res.status(403).json({ error: 'Ingesta local protegida' });
    try {
      const input = req.body.source === 'antigravity' ? antigravity(req.body) : req.body;
      const result = db.transaction(() => insertEvent(db, input)).immediate();
      if (result === 'inserted') broadcast();
      res.status(result === 'conflict' ? 409 : 200).json({ result });
    } catch {
      res.status(422).json({ error: 'Métricas o semántica no verificables' });
    }
  });
  app.use((req, res, next) => {
    if (!equal(req.headers['x-usage-monitor-gateway'], gatewayKey))
      return res.status(403).json({ error: 'Gateway requerido' });
    next();
  });
  app.use((req, res, next) => {
    if (
      !['GET', 'HEAD'].includes(req.method) &&
      (req.headers.origin !== origin ||
        req.headers['content-type']?.split(';')[0] !== 'application/json')
    )
      return res.status(403).json({ error: 'Origen o formato no permitido' });
    // An external link may open the entry screen; cross-site API, iframe and mutation requests remain denied.
    const entryNavigation =
      req.method === 'GET' &&
      ['/', '/access'].includes(req.path) &&
      req.headers['sec-fetch-mode'] === 'navigate' &&
      req.headers['sec-fetch-dest'] === 'document';
    if (req.headers['sec-fetch-site'] === 'cross-site' && !entryNavigation)
      return res.status(403).json({ error: 'Origen no permitido' });
    next();
  });
  const clear = (res) =>
    res.cookie(cookieName, '', {
      httpOnly: true,
      secure: true,
      sameSite: 'strict',
      path: '/',
      maxAge: 0,
    });
  app.post('/auth/exchange', (req, res) => {
    // A global persisted cap avoids trusting spoofable forwarding/IP headers.
    const key = 'exchange',
      a = db.prepare('SELECT * FROM attempts WHERE key=?').get(key);
    if (a && a.until > now() && a.count >= 20)
      return res
        .status(429)
        .set('Retry-After', '60')
        .json({ error: 'Demasiados intentos. Espera un minuto.' });
    db.prepare(
      'INSERT INTO attempts VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET count=excluded.count,until=excluded.until',
    ).run(
      key,
      a && a.until > now() ? a.count + 1 : 1,
      a && a.until > now() ? a.until : now() + 60000,
    );
    if (typeof req.body.token !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(req.body.token))
      return res.status(401).json({ error: 'Token inválido, utilizado o caducado' });
    const s = exchange(db, req.body.token, now());
    if (!s) return res.status(401).json({ error: 'Token inválido, utilizado o caducado' });
    res
      .cookie(cookieName, s.session, {
        httpOnly: true,
        secure: true,
        sameSite: 'strict',
        path: '/',
        expires: new Date(s.expiresAt),
      })
      .json({ csrf: s.csrf, expiresAt: s.expiresAt });
  });
  app.get('/auth/session', (req, res) => {
    const s = authenticate(req);
    if (!s) {
      clear(res);
      return res.status(401).json({ error: 'Acceso caducado o no iniciado' });
    }
    res.json({ csrf: s.csrf, expiresAt: new Date(s.expires).toISOString() });
  });
  // Only the isolated access screen and its stylesheet/script are public.
  app.get('/access', (_req, res) => res.sendFile(resolve(dist, 'access.html')));
  app.get('/access.js', (_req, res) => res.sendFile(resolve(dist, 'access.js')));
  app.get('/access.css', (_req, res) => res.sendFile(resolve(dist, 'access.css')));
  app.use((req, res, next) => {
    req.session = authenticate(req);
    if (!req.session) {
      if (req.path === '/' && req.method === 'GET') return res.redirect(303, '/access');
      clear(res);
      return res.status(401).json({ error: 'Acceso caducado o no iniciado' });
    }
    next();
  });
  app.post('/auth/logout', (req, res) => {
    if (!equal(req.headers['x-csrf-token'], req.session.csrf))
      return res.status(403).json({ error: 'CSRF inválido' });
    db.prepare('DELETE FROM sessions WHERE hash=?').run(req.session.hash);
    clear(res);
    res.json({ ok: true });
  });
  app.get('/api/analytics', (req, res) => {
    try {
      res.json({
        ...analytics(db, filters(req.query, now())),
        updatedAt: new Date(now()).toISOString(),
        activation: new Date(
          Number(db.prepare("SELECT value FROM meta WHERE key='activation'").get().value),
        ).toISOString(),
        collector: collector.status,
        errors: db.prepare('SELECT kind,at FROM errors ORDER BY at DESC LIMIT 20').all(),
        errorCount: db.prepare('SELECT COUNT(*) n FROM errors').get().n,
      });
    } catch {
      res.status(400).json({ error: 'Filtros inválidos' });
    }
  });
  app.get('/api/sources', (_req, res) =>
    res.json({
      codex: collector.status,
      ...sources?.status,
      tokenizer: tokenizer?.status ?? { state: 'not_configured' },
    }),
  );
  app.get('/api/reconciliation', (req, res) => {
    try {
      res.json(reconcile(db, filters(req.query, now())));
    } catch {
      res.status(400).json({ error: 'Filtros inválidos' });
    }
  });
  app.get('/api/events', (req, res) => {
    try {
      res.json(events(db, filters(req.query, now()), req.query));
    } catch {
      res.status(400).json({ error: 'Consulta inválida' });
    }
  });
  app.get('/api/stream', (req, res) => {
    res.set({
      'Content-Type': 'text/event-stream',
      Connection: 'keep-alive',
      'X-Accel-Buffering': 'no',
    });
    res.flushHeaders();
    res.write(`event: ready\ndata: ${revision}\n\n`);
    const s = { res, hash: req.session.hash };
    streams.add(s);
    const beat = setInterval(() => res.write(': heartbeat\n\n'), 15000);
    req.on('close', () => {
      clearInterval(beat);
      streams.delete(s);
    });
  });
  app.use(express.static(dist, { index: false, fallthrough: true, dotfiles: 'deny' }));
  app.get('/', (_req, res) => res.sendFile(resolve(dist, 'index.html')));
  app.use((_req, res) => res.status(404).json({ error: 'No encontrado' }));
  app.use((err, req, res, _next) =>
    res.status(err.type === 'entity.too.large' ? 413 : 400).json({ error: 'Solicitud inválida' }),
  );
  return {
    app,
    broadcast,
    close: () => {
      clearInterval(sweep);
      for (const s of streams) s.res.end();
    },
  };
}
