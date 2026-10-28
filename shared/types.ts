export interface UsageInput {
  agent_name: string;
  model: string;
  prompt_tokens: number;
  completion_tokens: number;
  request_id?: string;
}

export interface AppliedPricing {
  provider: string;
  source: string;
  verified_at: string;
  effective_from: string;
  input_per_million: string;
  output_per_million: string;
  long_context: boolean;
}

export interface UsageEvent extends Omit<UsageInput, 'request_id'> {
  id: number;
  request_id: string | null;
  received_at: string;
  input_cost_usd: string | null;
  output_cost_usd: string | null;
  cost_usd: string | null;
  pricing: AppliedPricing | null;
}

export interface Totals {
  requests: number;
  prompt_tokens: string;
  completion_tokens: string;
  total_tokens: string;
  cost_usd: string;
  unpriced_requests: number;
}

export interface Group extends Totals { name: string }
export interface Summary {
  totals: Totals;
  agents: Group[];
  models: Group[];
}
export interface Snapshot extends Summary {
  events: UsageEvent[];
  last_event_id: number;
  next_before_id: number | null;
}
export interface LiveUpdate extends Summary { event: UsageEvent }
export interface EventPage { events: UsageEvent[]; next_before_id: number | null }

export interface Rate {
  effective_from: string;
  input_per_million: string;
  output_per_million: string;
  long_context?: { above_prompt_tokens: number; input_per_million: string; output_per_million: string };
}
export interface PricingCatalog {
  currency: 'USD';
  verified_at: string;
  models: Record<string, { provider: string; source: string; rates: Rate[] }>;
}
export interface PricingResponse extends PricingCatalog { server_date: string }
