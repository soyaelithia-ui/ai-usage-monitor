import type {
  AnalyticsResponse,
  DiscoveryResponse,
  EventsResponse,
  ReconciliationResponse,
  SharedFilters,
  SourcesResponse,
} from './types';

export class UnauthorizedError extends Error {
  constructor() {
    super('Sesión caducada o no iniciada');
  }
}

async function apiGet<T>(url: string): Promise<T> {
  const res = await fetch(url, {
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (res.status === 401) {
    throw new UnauthorizedError();
  }
  if (!res.ok) {
    let msg = `Error HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {}
    throw new Error(msg);
  }
  return (await res.json()) as T;
}

export function buildFilterQuery(filters: SharedFilters, extra: Record<string, string | number> = {}): string {
  const params = new URLSearchParams();
  params.set('window', filters.window);
  if (filters.agent) params.set('agent', filters.agent);
  if (filters.model) params.set('model', filters.model);
  if (filters.provider) params.set('provider', filters.provider);
  if (filters.sessionId) params.set('sessionId', filters.sessionId);
  for (const [k, v] of Object.entries(extra)) {
    params.set(k, String(v));
  }
  return params.toString();
}

export async function checkSession(): Promise<{ csrf: string; expiresAt: string } | null> {
  const res = await fetch('/auth/session', {
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (!res.ok) return null;
  return (await res.json()) as { csrf: string; expiresAt: string };
}

export async function exchangeToken(token: string): Promise<{ csrf: string; expiresAt: string }> {
  const res = await fetch('/auth/exchange', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    cache: 'no-store',
    body: JSON.stringify({ token }),
  });
  if (!res.ok) {
    let msg = 'No se pudo autenticar la sesión';
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {}
    throw new Error(msg);
  }
  return (await res.json()) as { csrf: string; expiresAt: string };
}

export async function logoutSession(csrf: string): Promise<void> {
  await fetch('/auth/logout', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf,
    },
    credentials: 'same-origin',
    cache: 'no-store',
    body: JSON.stringify({}),
  });
}

export function fetchAnalytics(filters: SharedFilters): Promise<AnalyticsResponse> {
  return apiGet<AnalyticsResponse>(`/api/analytics?${buildFilterQuery(filters)}`);
}

export function fetchEvents(
  filters: SharedFilters,
  limit = 50,
  offset = 0,
): Promise<EventsResponse> {
  return apiGet<EventsResponse>(`/api/events?${buildFilterQuery(filters, { limit, offset })}`);
}

export function fetchSources(): Promise<SourcesResponse> {
  return apiGet<SourcesResponse>('/api/sources');
}

export function fetchDiscovery(): Promise<DiscoveryResponse> {
  return apiGet<DiscoveryResponse>('/api/discovery');
}

export function fetchReconciliation(filters: SharedFilters): Promise<ReconciliationResponse> {
  return apiGet<ReconciliationResponse>(`/api/reconciliation?${buildFilterQuery(filters)}`);
}
