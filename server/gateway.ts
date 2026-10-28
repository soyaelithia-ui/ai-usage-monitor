import { createServer, request, type IncomingMessage, type ServerResponse } from 'node:http';
import { timingSafeEqual } from 'node:crypto';

function authorized(header: string | undefined, token: string) {
  const supplied = Buffer.from(header ?? '', 'utf8');
  const expected = Buffer.from(`Bearer ${token}`, 'utf8');
  return supplied.length === expected.length && timingSafeEqual(supplied, expected);
}

/** Public, read-only dashboard plus token-protected ingestion; upstream remains local. */
export function createGateway(options: { upstreamPort: number; token: string }) {
  if (options.token.length < 32) throw new Error('El token de ingesta debe tener al menos 32 caracteres');
  return createServer((req: IncomingMessage, res: ServerResponse) => {
    const pathname = new URL(req.url ?? '/', 'http://localhost').pathname;
    const read = req.method === 'GET' || req.method === 'HEAD';
    const ingest = req.method === 'POST' && pathname === '/api/usage';
    if (!read && !ingest) {
      res.writeHead(405, { 'Content-Type': 'application/json', 'Allow': 'GET, HEAD, POST' });
      res.end(JSON.stringify({ error: 'Método no permitido' }));
      return;
    }
    if (ingest && !authorized(req.headers.authorization, options.token)) {
      res.writeHead(401, { 'Content-Type': 'application/json', 'WWW-Authenticate': 'Bearer', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ error: 'Se requiere un token de ingesta válido' }));
      return;
    }
    const headers = { ...req.headers, host: `127.0.0.1:${options.upstreamPort}` };
    // The upstream only accepts local origins. The public gateway deliberately
    // has no CORS headers and authenticates all writes independently.
    delete headers.origin;
    delete headers.authorization;
    const upstream = request({ host: '127.0.0.1', port: options.upstreamPort, method: req.method, path: req.url, headers }, proxyResponse => {
      res.writeHead(proxyResponse.statusCode ?? 502, proxyResponse.headers);
      proxyResponse.pipe(res);
    });
    upstream.on('error', () => {
      if (!res.headersSent) res.writeHead(502, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ error: 'El servicio local no está disponible' }));
    });
    res.on('close', () => upstream.destroy());
    req.pipe(upstream);
  });
}
