import Decimal from 'decimal.js';
import {
  agents,
  pricing,
  longContext,
  pricingSchedule,
  pricingNote,
  pricingRules,
  providerNotes,
} from './catalog.js';
Decimal.set({ precision: 100 });
export { Decimal, pricingNote };
export function integer(v, optional = false) {
  if (v === null || v === undefined) {
    if (optional) return null;
    throw Error('missing_metric');
  }
  if (typeof v === 'number' && !Number.isSafeInteger(v)) throw Error('unsafe_integer');
  const s = String(v);
  if (!/^(0|[1-9][0-9]{0,39})$/.test(s)) throw Error('invalid_integer');
  return s;
}
function label(v, fallback) {
  if (v == null && fallback !== undefined) return fallback;
  if (typeof v !== 'string' || !v.length || v.length > 160 || /[\x00-\x1f]/.test(v))
    throw Error('invalid_label');
  return v;
}
export function normalize(x) {
  const occurredAt = new Date(x.occurredAt ?? x.timestamp);
  if (!Number.isFinite(occurredAt.getTime())) throw Error('invalid_timestamp');
  const e = {
    source: label(x.source, 'local'),
    provider: label(x.provider, 'unknown'),
    agent: label(x.agent, 'unknown'),
    model: label(x.model, 'unknown'),
    responseId: label(x.responseId ?? x.id),
    occurredAt: occurredAt.toISOString(),
    inputTokens: integer(x.inputTokens),
    outputTokens: integer(x.outputTokens),
    cachedInputTokens: integer(x.cachedInputTokens, true),
    cacheWriteInputTokens: integer(x.cacheWriteInputTokens, true),
    reasoningOutputTokens: integer(x.reasoningOutputTokens, true),
    cacheWriteDuration: x.cacheWriteDuration ?? null,
  };
  if (![null, '30m', '12h'].includes(e.cacheWriteDuration)) throw Error('invalid_cache_duration');
  if (
    BigInt(e.cachedInputTokens ?? 0) + BigInt(e.cacheWriteInputTokens ?? 0) >
      BigInt(e.inputTokens) ||
    BigInt(e.reasoningOutputTokens ?? 0) > BigInt(e.outputTokens)
  )
    throw Error('invalid_breakdown');
  e.totalTokens = (BigInt(e.inputTokens) + BigInt(e.outputTokens)).toString();
  return e;
}
const unestimated = (costNote) => ({
  costUsd: null,
  costInputUsd: null,
  costCacheUsd: null,
  costOutputUsd: null,
  costNote,
});
export function estimate(e, customPricing) {
  let r = (customPricing && customPricing[e.model]) || pricing[e.model];
  if (!r || (r.provider && e.provider && r.provider !== e.provider))
    return unestimated(pricingRules.unknownRate);
  // Scheduled price changes; the latest entry whose date has passed wins.
  for (const change of pricingSchedule[e.model] ?? [])
    if (Date.parse(e.occurredAt) >= Date.parse(change.from)) r = { ...r, ...change };
  if (e.cachedInputTokens === null || e.cacheWriteInputTokens === null)
    return unestimated(pricingRules.incompleteCache);
  if (r.modality && e.modality !== r.modality) return unestimated(pricingRules.modalityUnverified);
  if (r.cacheWrite === undefined && BigInt(e.cacheWriteInputTokens) > 0n)
    return unestimated(pricingRules.cacheWriteUnavailable);
  const lc = longContext[e.model],
    long = !!lc && BigInt(e.inputTokens) > BigInt(lc.threshold),
    d = (x) => new Decimal(x),
    scale = long ? lc.input : 1;
  const cacheWriteRate =
    e.cacheWriteDuration === '12h' ? (r.cacheWrite12h ?? r.cacheWrite) : r.cacheWrite;
  const cacheRead = d(e.cachedInputTokens)
    .times(r.cached ?? r.input)
    .times(scale)
    .div(1e6);
  const cacheWrite = d(e.cacheWriteInputTokens)
    .times(cacheWriteRate ?? 0)
    .times(scale)
    .div(1e6);
  const cache = cacheRead.plus(cacheWrite);
  const normal = d(e.inputTokens)
    .minus(e.cachedInputTokens)
    .minus(e.cacheWriteInputTokens)
    .times(r.input)
    .times(scale)
    .div(1e6);
  const output = d(e.outputTokens)
    .times(r.output)
    .times(long ? lc.output : 1)
    .div(1e6);
  return {
    costCacheReadUsd: cacheRead.toFixed(),
    costCacheWriteUsd: cacheWrite.toFixed(),
    costReasoningUsd:
      e.reasoningOutputTokens === null
        ? null
        : d(e.reasoningOutputTokens)
            .times(r.output)
            .times(long ? lc.output : 1)
            .div(1e6)
            .toFixed(),
    costVisibleOutputUsd:
      e.reasoningOutputTokens === null
        ? null
        : d(e.outputTokens)
            .minus(e.reasoningOutputTokens)
            .times(r.output)
            .times(long ? lc.output : 1)
            .div(1e6)
            .toFixed(),
    costUsd: normal.plus(cache).plus(output).toFixed(),
    costInputUsd: normal.toFixed(),
    costCacheUsd: cache.toFixed(),
    costOutputUsd: output.toFixed(),
    costNote:
      r.cacheWrite === undefined && providerNotes[r.provider]
        ? providerNotes[r.provider]
        : BigInt(e.cacheWriteInputTokens) > 0n && !e.cacheWriteDuration
          ? pricingRules.cacheWriteAssumed
          : pricingRules.assumedStandard,
  };
}
const selfReported = agents.filter((a) => a.reader === 'contract');
export function contractMessage(record, agentDef) {
  if (
    record?.schema !== 'usage-monitor.usage.v1' ||
    record?.semantics !== 'input_includes_cache;output_includes_reasoning' ||
    !record.model ||
    !record.provider
  )
    throw Error(`${agentDef?.id ?? 'contract'}_unverified_schema`);
  const agentId = agentDef?.id ?? record.source ?? 'custom';
  const agentName =
    agentDef?.name ?? record.agent ?? agentId.slice(0, 1).toUpperCase() + agentId.slice(1);
  return normalize({ ...record, source: agentId, agent: agentName });
}
export function antigravity(record) {
  return contractMessage(record, selfReported[0]);
}
