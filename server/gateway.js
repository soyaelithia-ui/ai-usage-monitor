import http from 'node:http';
import { security } from './app.js';
export function gateway(c) {
  return http.createServer((req, res) => {
    security(
      req,
      {
        set: (h) => {
          for (const [k, v] of Object.entries(h)) res.setHeader(k, v);
        },
      },
      () => {},
    );
    const url = new URL(c.origin),
      host = url.host;
    // Only the trusted loopback reverse proxy may assert the configured HTTPS origin.
    const peerOK = ['127.0.0.1', '::1', '::ffff:127.0.0.1'].includes(req.socket.remoteAddress);
    const proxyOK =
      req.headers.host === `127.0.0.1:${c.gatewayPort}` &&
      req.headers['x-forwarded-host'] === host &&
      req.headers['x-forwarded-proto'] === 'https';
    const localTest =
      url.protocol === 'http:' &&
      req.headers.host === host &&
      !req.headers['x-forwarded-host'] &&
      !req.headers['x-forwarded-proto'];
    if (!peerOK || !(proxyOK || localTest)) {
      res.writeHead(421);
      return res.end('Invalid proxy origin');
    }
    let path;
    try {
      path = new URL(req.url, c.origin).pathname;
    } catch {
      res.writeHead(400);
      return res.end();
    }
    if (
      !['GET', 'HEAD', 'POST'].includes(req.method) ||
      path === '/api/ingest' ||
      (![
        '/',
        '/access',
        '/access.js',
        '/access.css',
        '/auth/session',
        '/auth/exchange',
        '/auth/logout',
        '/api/analytics',
        '/api/events',
        '/api/stream',
        '/api/sources',
        '/api/reconciliation',
      ].includes(path) &&
        !path.startsWith('/assets/'))
    ) {
      res.writeHead(404);
      return res.end();
    }
    const headers = { host: '127.0.0.1', 'x-usage-monitor-gateway': c.gatewayKey };
    for (const h of [
      'cookie',
      'content-type',
      'content-length',
      'origin',
      'sec-fetch-site',
      'sec-fetch-mode',
      'sec-fetch-dest',
      'x-csrf-token',
      'last-event-id',
    ])
      if (req.headers[h]) headers[h] = req.headers[h];
    const upstream = http.request(
      { host: '127.0.0.1', port: c.backendPort, path: req.url, method: req.method, headers },
      (r) => {
        res.writeHead(r.statusCode, r.headers);
        r.pipe(res);
      },
    );
    upstream.on('error', () => {
      if (!res.headersSent) res.writeHead(502);
      res.end();
    });
    res.on('close', () => upstream.destroy());
    req.pipe(upstream);
  });
}
