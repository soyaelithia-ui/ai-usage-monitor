import express from 'express';
import { timingSafeEqual } from 'node:crypto';
import { resolve } from 'node:path';
import {
  hash,
  exchange,
  filters,
  events,
  analytics,
  insertEvent,
  insertDynamicAgent,
  getDynamicAgents,
  setDynamicPricing,
} from './db.js';
import { antigravity, contractMessage } from './usage.js';
import { agents } from './catalog.js';
import { reconcile } from './provenance.js';
import { discoverLocalTechnologies } from './discovery.js';
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
    'Cross-Origin-Opener-Policy': 'same-origin',
    'Cross-Origin-Resource-Policy': 'same-origin',
    'X-DNS-Prefetch-Control': 'off',
  });
  next();
}
export function createApp({
  db,
  origin,
  gatewayKey,
  ingestKey,
  collector,
  collectors,
  sources,
  tokenizer,
  dist = resolve('dist'),
  now = Date.now,
}) {
  const app = express();
  app.disable('x-powered-by');
  app.set('trust proxy', false);
  app.use(security);
  // One JSONL collector per catalogue agent; tests may pass a single one.
  const readerList = collectors ?? (collector ? [collector] : []);
  const readerStatus = () =>
    Object.fromEntries(readerList.map((c) => [c.agent?.id ?? 'codex', c.status ?? c]));
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
      let input;
      if (req.body?.schema === 'usage-monitor.usage.v1') {
        const sourceId =
          typeof req.body.source === 'string' && /^[a-zA-Z0-9_-]{1,64}$/.test(req.body.source)
            ? req.body.source
            : 'custom';
        const dynamicAgents = getDynamicAgents(db);
        let agentDef =
          dynamicAgents.find((a) => a.id === sourceId) || agents.find((a) => a.id === sourceId);
        if (!agentDef && sourceId) {
          const name =
            typeof req.body.agent === 'string' && req.body.agent.trim()
              ? req.body.agent.trim().slice(0, 100)
              : sourceId.slice(0, 1).toUpperCase() + sourceId.slice(1);
          const provider =
            typeof req.body.provider === 'string' && req.body.provider.trim()
              ? req.body.provider.trim().slice(0, 64)
              : 'custom';
          insertDynamicAgent(db, {
            id: sourceId,
            name,
            reader: 'contract',
            provider,
            created_at: Date.now(),
          });
          agentDef = { id: sourceId, name, reader: 'contract', provider };
        }
        input = contractMessage(req.body, agentDef);
      } else {
        input = req.body.source === 'antigravity' ? antigravity(req.body) : req.body;
      }
      const result = db.transaction(() => insertEvent(db, input)).immediate();
      if (result === 'inserted') broadcast();
      res.status(result === 'conflict' ? 409 : 200).json({ result });
    } catch {
      res.status(422).json({ error: 'Métricas o semántica no verificables' });
    }
  });
  app.post('/api/agents/register', (req, res) => {
    const isDirect = equal(req.headers.authorization, `Bearer ${ingestKey}`);
    const isGateway = equal(req.headers['x-usage-monitor-gateway'], gatewayKey);
    if (!isDirect && !isGateway) return res.status(403).json({ error: 'Autorización requerida' });
    try {
      const { id, name, reader, provider, tone, symbol, iconUrl, config, rates } = req.body || {};
      if (!id || typeof id !== 'string' || !/^[a-zA-Z0-9_-]{1,64}$/.test(id)) {
        return res.status(400).json({ error: 'Identificador de agente inválido' });
      }
      const cleanReader = ['contract', 'sqlite', 'jsonl'].includes(reader) ? reader : 'contract';
      const cleanName = typeof name === 'string' && name.trim() ? name.trim().slice(0, 100) : id;
      const cleanProvider =
        typeof provider === 'string' && provider.trim() ? provider.trim().slice(0, 64) : 'custom';
      const cleanTone = ['mint', 'violet', 'blue', 'amber', 'rose', 'cyan'].includes(tone)
        ? tone
        : null;
      const cleanSymbol =
        typeof symbol === 'string' && symbol.trim() ? symbol.trim().slice(0, 4) : null;
      const cleanIconUrl =
        typeof iconUrl === 'string' &&
        (iconUrl.startsWith('https://') || iconUrl.startsWith('data:image/') || iconUrl.startsWith('/')) &&
        iconUrl.length <= 2048
          ? iconUrl.trim()
          : (config?.iconUrl && typeof config.iconUrl === 'string' ? config.iconUrl.trim() : null);
      const cleanConfig = {
        ...(config && typeof config === 'object' && !Array.isArray(config) ? config : {}),
        ...(cleanIconUrl ? { iconUrl: cleanIconUrl } : {}),
      };
      insertDynamicAgent(db, {
        id,
        name: cleanName,
        reader: cleanReader,
        provider: cleanProvider,
        tone: cleanTone,
        symbol: cleanSymbol,
        config: cleanConfig,
        created_at: Date.now(),
      });
      if (rates && typeof rates === 'object' && !Array.isArray(rates)) {
        for (const [model, p] of Object.entries(rates)) {
          if (
            typeof model === 'string' &&
            model.length > 0 &&
            model.length <= 128 &&
            model !== '__proto__' &&
            model !== 'constructor' &&
            model !== 'prototype' &&
            p &&
            typeof p === 'object' &&
            !Array.isArray(p) &&
            p.input &&
            p.output
          ) {
            setDynamicPricing(db, { model, provider: p.provider || cleanProvider, ...p });
          }
        }
      }
      broadcast();
      res.json({ ok: true, agent: { id, name: cleanName, reader: cleanReader } });
    } catch {
      res.status(400).json({ error: 'Datos de registro inválidos' });
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
  app.get('/access', (_req, res) => res.sendFile('access.html', { root: dist }));
  app.get('/access.js', (_req, res) => res.sendFile('access.js', { root: dist }));
  app.get('/access.css', (_req, res) => res.sendFile('access.css', { root: dist }));
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
        collector: collectors ? readerStatus() : (collector?.status ?? { state: 'not_configured' }),
        errors: db.prepare('SELECT kind,at FROM errors ORDER BY at DESC LIMIT 20').all(),
        errorCount: db.prepare('SELECT COUNT(*) n FROM errors').get().n,
      });
    } catch {
      res.status(400).json({ error: 'Filtros inválidos' });
    }
  });
  app.get('/api/sources', (_req, res) => {
    const dynamic = getDynamicAgents(db);
    const dynamicMap = new Map(dynamic.map((a) => [a.id, a]));
    const merged = agents.map((a) =>
      dynamicMap.has(a.id) ? { ...a, ...dynamicMap.get(a.id) } : a,
    );
    for (const [id, a] of dynamicMap) {
      if (!agents.some((ag) => ag.id === id)) {
        merged.push({
          id: a.id,
          name: a.name,
          tone: a.tone || 'mint',
          symbol: a.symbol || a.name.slice(0, 1).toUpperCase(),
          reader: a.reader,
          iconUrl: a.config?.iconUrl,
        });
      }
    }
    res.json({
      ...readerStatus(),
      ...sources?.status,
      tokenizer: tokenizer?.status ?? { state: 'not_configured' },
      agents: merged.map(({ id, name, tone, symbol, reader, config, iconUrl }) => ({
        id,
        name,
        tone: tone || 'mint',
        symbol: symbol || name.slice(0, 1).toUpperCase(),
        reader,
        iconUrl: iconUrl || config?.iconUrl,
      })),
    });
  });
  app.get('/api/discovery', (_req, res) => {
    try {
      const technologies = discoverLocalTechnologies(process.env, sources?.paths ?? {});
      res.json({ technologies });
    } catch {
      res.json({ technologies: [] });
    }
  });
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
  app.get('/', (_req, res) => res.sendFile('index.html', { root: dist }));
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
