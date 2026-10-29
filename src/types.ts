/** Integers are decimal strings on the wire to preserve values above 2^53. */
export interface UsageInput extends Partial<UsageProvenance> {
  id?: string;
  responseId: string;
  agent: string;
  model?: string;
  source?: string;
  provider?: string;
  occurredAt: string;
  timestamp?: string;
  inputTokens: string | number;
  outputTokens: string | number;
  cachedInputTokens?: string | number | null;
  cacheWriteInputTokens?: string | number | null;
  reasoningOutputTokens?: string | number | null;
  cacheWriteDuration?: '30m' | '12h' | null;
}
export interface UsageEvent extends Omit<UsageInput, 'id'> {
  costCacheReadUsd: string | null;
  costCacheWriteUsd: string | null;
  costVisibleOutputUsd: string | null;
  costReasoningUsd: string | null;
  id: number;
  totalTokens: string;
  costUsd: string | null;
  costInputUsd: string | null;
  costCacheUsd: string | null;
  costOutputUsd: string | null;
  costNote: string;
}

/** Provenance is separate from the canonical usage fingerprint. */
export interface UsageProvenance {
  method: 'native' | 'reported';
  sessionId: string | null;
  turnId: string | null;
  sourceVersion: string | null;
  coverage: string;
  modality: string;
  pricingVersion: string;
  serviceTier: 'standard_assumed';
}
export interface TextMeasurement {
  method: 'local_text';
  coverage: 'visible_output_text_only';
  tokens: string | null;
  tokenizer: string | null;
  version: string | null;
  status: string;
  comparison: 'not_comparable_to_full_native_usage';
}

export interface AgentInterval {
  at: string;
  count: number;
  inputTokens: string;
  outputTokens: string;
  costUsd: string;
}
export interface AgentPanel {
  name: string;
  count: number;
  totalTokens: string;
  inputTokens: string;
  outputTokens: string;
  costUsd: string;
  unpriced: number;
  lastEventAt: string;
  buckets: AgentInterval[];
}
