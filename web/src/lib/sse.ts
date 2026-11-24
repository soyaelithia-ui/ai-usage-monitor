export interface StreamHandlers {
  tokenRequired?: boolean;
  onStatus: (live: boolean) => void;
  onChange: () => void;
  onExpired: () => void;
}

export function connectTelemetryStream(handlers: StreamHandlers): () => void {
  const es = new EventSource('/api/stream', { withCredentials: true });
  let closed = false;

  const verifyExpired = async () => {
    try {
      const r = await fetch('/auth/session', { credentials: 'same-origin', cache: 'no-store' });
      if ((r.status === 401 || r.status === 403) && !closed) {
        closed = true;
        handlers.onStatus(false);
        es.close();
        handlers.onExpired();
      }
    } catch {}
  };

  es.addEventListener('ready', () => {
    if (!closed) handlers.onStatus(true);
  });

  es.addEventListener('change', () => {
    if (!closed) handlers.onChange();
  });

  es.addEventListener('expired', () => {
    if (closed) return;
    if (handlers.tokenRequired === false) {
      handlers.onStatus(false);
      void verifyExpired();
      return;
    }
    closed = true;
    handlers.onStatus(false);
    es.close();
    handlers.onExpired();
  });

  es.onerror = () => {
    if (closed) return;
    handlers.onStatus(false);
    void verifyExpired();
  };

  return () => {
    closed = true;
    handlers.onStatus(false);
    es.close();
  };
}
