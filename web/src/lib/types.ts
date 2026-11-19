export type WindowOption = '15m' | '1h' | '24h' | '7d';
export type ViewTab = 'overview' | 'history' | 'sources';
export type ThemeId = 'bioluminescent' | 'cute-pastel-blue' | 'burned-paper' | 'vivid-yellow-green';
export type ThemeMode = 'dark' | 'light';

export interface SummaryMetrics {
  id?: string;
  name?: string;
  at?: string;
  count: number;
  inputTokens: string;
  outputTokens: string;
  totalTokens: string;
  cachedInputTokens: string;
  cacheWriteInputTokens: string;
  reasoningOutputTokens: string;
  normalInputTokens: string;
  visibleOutputTokens: string;
  costCacheReadUsd: string;
  costCacheWriteUsd: string;
  costVisibleOutputUsd: string;
  costReasoningUsd: string;
  costInputUsd: string;
  costCacheUsd: string;
  costOutputUsd: string;
  costUsd: string;
  cumulativeCostUsd?: string;
  missingCache: number;
  missingReasoning: number;
  unpriced: number;
}

export interface AgentBucket {
  at: string;
  count: number;
  inputTokens: string;
  outputTokens: string;
  costUsd: string;
}

export interface AgentPanel extends SummaryMetrics {
  id?: string;
  name: string;
  lastEventAt: string;
  buckets: AgentBucket[];
}

export interface CollectorStatus {
  state: string;
  reason?: string;
  lastScan?: string;
  lastEvent?: string;
  files?: number;
  recordsRead?: number;
  version?: string;
}

export interface FilterOptions {
  agents?: string[];
  models?: string[];
  providers?: string[];
  sessions?: string[];
  methods?: string[];
}

export interface AnalyticsResponse {
  from: number;
  to: number;
  window: WindowOption;
  intervalMs: number;
  totals: SummaryMetrics;
  buckets: SummaryMetrics[];
  agents: SummaryMetrics[];
  models: SummaryMetrics[];
  agentPanels: AgentPanel[];
  options: FilterOptions;
  pricingNote: string;
  updatedAt: string;
  activation: string;
  collector: Record<string, CollectorStatus>;
  errors: Array<{ kind: string; at: string }>;
  errorCount: number;
}

export interface UsageEvent {
  id: number;
  source: string;
  provider: string;
  agent: string;
  model: string;
  responseId: string;
  occurredAt: string;
  inputTokens: string;
  outputTokens: string;
  cachedInputTokens: string | null;
  cacheWriteInputTokens: string | null;
  reasoningOutputTokens: string | null;
  cacheWriteDuration: string | null;
  totalTokens: string;
  costUsd: string | null;
  costInputUsd: string | null;
  costCacheUsd: string | null;
  costOutputUsd: string | null;
  costCacheReadUsd?: string | null;
  costCacheWriteUsd?: string | null;
  costReasoningUsd?: string | null;
  costVisibleOutputUsd?: string | null;
  costNote: string;
  method?: string;
  sessionId?: string;
  coverage?: string;
}

export interface EventsResponse {
  events: UsageEvent[];
  total: number;
  offset: number;
  limit: number;
  to: number;
  options?: FilterOptions;
}

export interface SourceAgent {
  id: string;
  name: string;
  tone: string;
  symbol: string;
  reader: string;
  iconUrl?: string;
}

export interface SourcesResponse {
  agents: SourceAgent[];
  errors?: Array<{ kind: string; at: string }>;
  errorCount?: number;
  [agentId: string]: unknown;
}

export interface DiscoveryResponse {
  technologies: Array<{
    id: string;
    name: string;
    reader: string;
    tone: string;
    symbol: string;
    detected: boolean;
    configured: boolean;
    carrier?: string;
  }>;
  models: Array<{
    model: string;
    priced: boolean;
  }>;
}

export interface ReconciliationResponse {
  responses: number;
  matched: number;
  conflicts: number;
  unverified: number;
  lastObserved: string | null;
  sessions: Array<{
    sessionId: string;
    count: number;
    inputTokens: string;
    outputTokens: string;
    cachedInputTokens: string;
  }>;
  textMeasurements?: Array<Record<string, unknown>>;
  note: string;
}

export interface SharedFilters {
  window: WindowOption;
  agent: string;
  model: string;
  provider: string;
  sessionId: string;
}
