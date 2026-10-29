import Decimal from 'decimal.js';
Decimal.set({ precision: 100 });
export { Decimal };
export const pricing = {
  'gpt-6-astra': ['10', '1', '12.5', '25', '50'],
  'gpt-6.1-sol': ['2', '.1', '2.5', '5', '10'],
  'gpt-6.1-terra': ['1', '.05', '1.25', '2.5', '5'],
  'gpt-6-luna': ['.1', '.01', '.125', '.25', '.5'],
  'gpt-6-sol': ['2', '.2', '2.5', '5', '10'],
};
export const googlePricing = {
  'gemini-2.5-pro': ['1.25', '.125', null, null, '10'],
  'gemini-2.5-flash': ['.30', '.03', null, null, '2.50'],
  'gemini-2.5-flash-lite': ['.10', '.01', null, null, '.40'],
  'gemini-4-argon': ['2', '.10', null, null, '10'],
  'gemini-3.8-flash': ['.75', '.075', null, null, '3.75'],
};
export const pricingNote =
  'Equivalente API Standard, USD; nivel supuesto, no cargo real. El contexto se cuenta en cada llamada, incluso cuando se reutiliza. Tarifas consultadas el 29-10-2026; no representa la factura de ChatGPT/Codex. Caché escrita sin duración: se supone 30 min. Sin recargos regionales ni herramientas. OpenAI: contexto >272.000, entrada/caché ×2 y salida ×1,5. Google Pro 2.5: umbral 200.000; Flash con modalidad desconocida queda sin estimar. Google excluye almacenamiento y herramientas.';
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
export function estimate(e) {
  let r =
    e.provider === 'openai'
      ? pricing[e.model]
      : e.provider === 'google'
        ? googlePricing[e.model]
        : null;
  if (e.provider === 'google' && r) {
    r = [...r];
    const date = Date.parse(e.occurredAt);
    if (e.model === 'gemini-4-argon' && date >= Date.parse('2026-12-01T00:00:00Z'))
      r = ['4', '.20', null, null, '20'];
    if (e.model === 'gemini-3.8-flash' && date >= Date.parse('2027-01-01T00:00:00Z'))
      r = ['1.50', '.15', null, null, '7.50'];
    if (['gemini-2.5-flash', 'gemini-2.5-flash-lite'].includes(e.model) && e.modality !== 'text')
      return {
        costUsd: null,
        costInputUsd: null,
        costCacheUsd: null,
        costOutputUsd: null,
        costNote: 'Modalidad de entrada no verificada: la tarifa de audio difiere',
      };
    if (BigInt(e.cacheWriteInputTokens ?? 0) > 0n)
      return {
        costUsd: null,
        costInputUsd: null,
        costCacheUsd: null,
        costOutputUsd: null,
        costNote: 'Escritura/almacenamiento de caché de Google no verificable',
      };
  }
  if (!r || e.cachedInputTokens === null || e.cacheWriteInputTokens === null)
    return {
      costUsd: null,
      costInputUsd: null,
      costCacheUsd: null,
      costOutputUsd: null,
      costNote: !r ? 'Tarifa no verificada' : 'Desglose de caché incompleto',
    };
  const long =
      (e.provider === 'openai' && BigInt(e.inputTokens) > 272000n) ||
      (e.provider === 'google' && e.model === 'gemini-2.5-pro' && BigInt(e.inputTokens) > 200000n),
    d = (x) => new Decimal(x),
    scale = long ? 2 : 1;
  const normal = d(e.inputTokens)
    .minus(e.cachedInputTokens)
    .minus(e.cacheWriteInputTokens)
    .times(r[0])
    .times(scale)
    .div(1e6);
  const cacheRead = d(e.cachedInputTokens).times(r[1]).times(scale).div(1e6);
  const cacheWrite = d(e.cacheWriteInputTokens)
    .times(r[e.cacheWriteDuration === '12h' ? 3 : 2] ?? 0)
    .times(scale)
    .div(1e6);
  const cache = d(e.cachedInputTokens)
    .times(r[1])
    .plus(d(e.cacheWriteInputTokens).times(r[e.cacheWriteDuration === '12h' ? 3 : 2] ?? 0))
    .times(scale)
    .div(1e6);
  const output = d(e.outputTokens)
    .times(r[4])
    .times(long ? '1.5' : 1)
    .div(1e6);
  return {
    costCacheReadUsd: cacheRead.toFixed(),
    costCacheWriteUsd: cacheWrite.toFixed(),
    costReasoningUsd:
      e.reasoningOutputTokens === null
        ? null
        : d(e.reasoningOutputTokens)
            .times(r[4])
            .times(long ? '1.5' : 1)
            .div(1e6)
            .toFixed(),
    costVisibleOutputUsd:
      e.reasoningOutputTokens === null
        ? null
        : d(e.outputTokens)
            .minus(e.reasoningOutputTokens)
            .times(r[4])
            .times(long ? '1.5' : 1)
            .div(1e6)
            .toFixed(),
    costUsd: normal.plus(cache).plus(output).toFixed(),
    costInputUsd: normal.toFixed(),
    costCacheUsd: cache.toFixed(),
    costOutputUsd: output.toFixed(),
    costNote:
      e.provider === 'google'
        ? 'API Standard supuesta; no incluye almacenamiento de caché, herramientas ni suscripción'
        : BigInt(e.cacheWriteInputTokens) > 0n && !e.cacheWriteDuration
          ? 'Escritura de caché: 30 min supuestos'
          : 'API Standard estimada',
  };
}
export function antigravity(record) {
  if (
    record?.schema !== 'usage-monitor.usage.v1' ||
    record?.semantics !== 'input_includes_cache;output_includes_reasoning' ||
    !record.model ||
    !record.provider
  )
    throw Error('antigravity_unverified_schema');
  return normalize({ ...record, source: 'antigravity', agent: 'Antigravity' });
}
