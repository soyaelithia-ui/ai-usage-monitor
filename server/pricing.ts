import { readFileSync } from 'node:fs';
import { Decimal } from 'decimal.js';
import { z } from 'zod';
import type { AppliedPricing, PricingCatalog, UsageInput } from '../shared/types.js';

// Enough precision for safe integer token counts and up to 12 fractional rate digits.
Decimal.set({ precision: 80, toExpNeg: -80, toExpPos: 80 });
const date = z.string().regex(/^\d{4}-\d{2}-\d{2}$/).refine(value => {
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}, 'Fecha UTC inválida');
const price = z.string().regex(/^\d{1,12}(\.\d{1,12})?$/, 'Precio decimal no negativo, como string');
const rate = z.object({
  effective_from: date,
  input_per_million: price,
  output_per_million: price,
  long_context: z.object({
    above_prompt_tokens: z.number().int().nonnegative().safe(),
    input_per_million: price,
    output_per_million: price,
  }).strict().optional(),
}).strict();
export const catalogSchema = z.object({
  currency: z.literal('USD'),
  verified_at: date,
  models: z.record(z.string().min(1).max(160), z.object({
    provider: z.string().min(1),
    source: z.string().url().refine(value => value.startsWith('https://')),
    rates: z.array(rate).min(1).refine(rates => rates.every((entry, index) =>
      index === 0 || entry.effective_from > rates[index - 1].effective_from),
    'Las fechas deben ser únicas y estar en orden ascendente'),
  }).strict()),
}).strict();

export function loadPricing(path: string): PricingCatalog {
  return catalogSchema.parse(JSON.parse(readFileSync(path, 'utf8')));
}

export function calculateCost(input: UsageInput, catalog: PricingCatalog, receivedAt: string) {
  const unknown = { pricing: null, input_cost_usd: null, output_cost_usd: null, cost_usd: null };
  const model = Object.hasOwn(catalog.models, input.model) ? catalog.models[input.model] : undefined;
  if (!model) return unknown;
  const day = receivedAt.slice(0, 10);
  const selected = model.rates.findLast(entry => entry.effective_from <= day);
  if (!selected) return unknown;
  const isLong = !!selected.long_context && input.prompt_tokens > selected.long_context.above_prompt_tokens;
  const rates = isLong ? selected.long_context! : selected;
  const inputCost = new Decimal(input.prompt_tokens).mul(rates.input_per_million).div(1_000_000);
  const outputCost = new Decimal(input.completion_tokens).mul(rates.output_per_million).div(1_000_000);
  const pricing: AppliedPricing = {
    provider: model.provider,
    source: model.source,
    verified_at: catalog.verified_at,
    effective_from: selected.effective_from,
    input_per_million: rates.input_per_million,
    output_per_million: rates.output_per_million,
    long_context: isLong,
  };
  return {
    pricing,
    input_cost_usd: inputCost.toFixed(),
    output_cost_usd: outputCost.toFixed(),
    cost_usd: inputCost.plus(outputCost).toFixed(),
  };
}
