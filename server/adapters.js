import { agents } from './catalog.js';
import { integer } from './usage.js';
const sum = (...values) => values.reduce((n, v) => n + BigInt(integer(v)), 0n).toString();
/** Primary counters must be present; breakdown counters may stay unknown. */
const counters = new Set([
  'inputTokens',
  'outputTokens',
  'cachedInputTokens',
  'cacheWriteInputTokens',
  'reasoningOutputTokens',
]);
const requiredCounters = new Set(['inputTokens', 'outputTokens']);
const meta = new Set(['occurredAt', 'occurredAtFallback', 'occurredAtFormat']);
function get(record, path) {
  let value = record;
  for (const key of path.split('.')) {
    if (value === null || typeof value !== 'object') return undefined;
    value = value[key];
  }
  return value;
}
/**
 * Build a usage event from a record, driven by a catalogue entry.
 * `context` carries values that live outside the record (row columns) and is
 * addressable as `context.<name>`. A list of paths adds the present values
 * together with a missing value counting as zero; the first path is the
 * primary counter and must be present. Optional fields stay unknown instead
 * of being defaulted to zero.
 */
export function catalogMessage(entry, record, context = {}) {
  if (entry.expectedVersion && context.version !== undefined) {
    if (context.version !== entry.expectedVersion) throw Error(`${entry.id}_unsupported_version`);
  }
  if (entry.accept?.length) {
    const accepted = entry.accept.some((rule) => {
      if (rule.type !== undefined && record.type !== rule.type) return false;
      if (rule.path !== undefined) {
        const v = get(record, rule.path);
        if (rule.equals !== undefined ? v !== rule.equals : v == null) return false;
      }
      return true;
    });
    if (!accepted) return null;
  }
  const resolve = (path) =>
    path.startsWith('context.') ? context[path.slice(8)] : get(record, path);
  for (const field of entry.require ?? [])
    if (resolve(entry.usage[field]) == null) throw Error(`${entry.id}_missing_${field}`);
  // A record with no counter at all carries no native usage.
  const counterSpecs = Object.entries(entry.usage).filter(
    ([field, spec]) => spec && !meta.has(field) && counters.has(field),
  );
  if (
    counterSpecs.length &&
    !counterSpecs.some(([, spec]) =>
      (Array.isArray(spec) ? spec : [spec]).some((p) => resolve(p) != null),
    )
  )
    return null;
  const usage = {};
  for (const [field, spec] of Object.entries(entry.usage)) {
    if (!spec || meta.has(field)) continue;
    const values = (Array.isArray(spec) ? spec : [spec]).map(resolve);
    if (counters.has(field)) {
      if (values[0] == null) {
        if (requiredCounters.has(field)) throw Error('missing_metric');
        continue; // breakdown stays unknown instead of being defaulted to zero
      }
      usage[field] = sum(...values.map((v) => (v == null ? 0 : v)));
    } else {
      const v = values.find((v) => v != null);
      if (v != null) usage[field] = v;
    }
  }
  if (!('inputTokens' in usage) && !('outputTokens' in usage)) return null;
  let occurredAt = resolve(entry.usage.occurredAt);
  if ((occurredAt == null || occurredAt === '') && entry.usage.occurredAtFallback)
    occurredAt = resolve(entry.usage.occurredAtFallback);
  if (occurredAt == null || occurredAt === '') return null;
  if (entry.usage.occurredAtFormat === 'epoch-ms') occurredAt = new Date(Number(occurredAt));
  return {
    source: entry.id,
    agent: entry.name,
    method: entry.method,
    sourceVersion: entry.sourceVersion,
    coverage: entry.coverage,
    provider: entry.provider,
    ...usage,
    occurredAt,
  };
}
export const openCodeEntry = agents.find((a) => a.reader === 'sqlite');
export function openCodeMessage(m, sessionId, version) {
  return catalogMessage(openCodeEntry, m, { sessionId, version });
}
export function visibleText(content) {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return null;
  if (
    content.some(
      (p) =>
        typeof p !== 'string' && (!(typeof p?.text === 'string') || (p.type && p.type !== 'text')),
    )
  )
    return null;
  return content.map((p) => (typeof p === 'string' ? p : p.text)).join('');
}
