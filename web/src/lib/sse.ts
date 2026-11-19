export interface StreamHandlers {
  onStatus: (live: boolean) => void;
  onChange: () => void;
  onExpired: () => void;
}

export function connectTelemetryStream(handlers: StreamHandlers): () => void {
  const es = new EventSource('/api/stream', { withCredentials: true });
  let closed = false;

  es.addEventListener('ready', () => {
    if (!closed) handlers.onStatus(true);
  });

  es.addEventListener('change', () => {
    if (!closed) handlers.onChange();
  });

  es.addEventListener('expired', () => {
    if (closed) return;
    closed = true;
    handlers.onStatus(false);
    es.close();
    handlers.onExpired();
  });

  es.onerror = async () => {
    if (closed) return;
    handlers.onStatus(false);
    try {
      const r = await fetch('/auth/session', { credentials: 'same-origin', cache: 'no-store' });
      if (r.status === 401 && !closed) {
        closed = true;
        es.close();
        handlers.onExpired();
      }
    } catch {}
  };

  return () => {
    closed = true;
    handlers.onStatus(false);
    es.close();
  };
}
