import type { ViewTab } from './types';

export type EndpointKind = 'analytics' | 'events' | 'sources' | 'discovery' | 'reconciliation';
export type RefreshReason = 'init' | 'tab' | 'filter' | 'sse' | 'page';

/**
 * Determines the minimal set of API endpoints required for the currently active view
 * so SSE updates, filter changes, pagination, and tab switches never over-fetch inactive tabs.
 */
export function requiredEndpointsForTab(
  tab: ViewTab,
  reason: RefreshReason,
  hasSources: boolean,
): Set<EndpointKind> {
  const req = new Set<EndpointKind>();

  if (tab === 'overview') {
    req.add('analytics');
    if (!hasSources && (reason === 'init' || reason === 'tab')) {
      req.add('sources');
    }
    return req;
  }

  if (tab === 'history') {
    req.add('events');
    if (!hasSources && (reason === 'init' || reason === 'tab')) {
      req.add('sources');
    }
    return req;
  }

  // tab === 'sources'
  req.add('reconciliation');
  if (reason !== 'filter') {
    req.add('sources');
    req.add('discovery');
  }
  return req;
}
