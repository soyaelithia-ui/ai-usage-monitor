import { integer } from './usage.js';
const sum = (...values) => values.reduce((n, v) => n + BigInt(integer(v)), 0n).toString();
export function openCodeMessage(m, sessionId, version) {
  if (version !== '2.1.4') throw Error('opencode_unsupported_version');
  if (m.type !== 'assistant' && m.type !== 'compaction') return null;
  if (
    (m.type === 'assistant' && !m.time?.completed) ||
    (m.type === 'compaction' && m.status !== 'completed')
  )
    return null;
  if (!m.model?.id || !m.model.providerID) throw Error('opencode_missing_model');
  if (!m.tokens) return null;
  const t = m.tokens;
  return {
    source: 'opencode',
    agent: 'OpenCode',
    provider: m.model.providerID === 'google' ? 'google' : m.model.providerID,
    model: m.model.id,
    responseId: m.id,
    sessionId,
    occurredAt: new Date(m.time.completed ?? m.time.created).toISOString(),
    method: 'native',
    sourceVersion: 'opencode.2.1.4',
    coverage: 'agent_normalized_usage_zero_defaults_possible',
    inputTokens: sum(t.input, t.cache?.read, t.cache?.write),
    outputTokens: sum(t.output, t.reasoning),
    cachedInputTokens: integer(t.cache?.read),
    cacheWriteInputTokens: integer(t.cache?.write),
    reasoningOutputTokens: integer(t.reasoning),
  };
}
export function geminiMessage(m, sessionId) {
  if (m.type !== 'gemini' || !m.tokens) return null;
  if (!m.id || !m.model) throw Error('gemini_missing_model_or_id');
  const t = m.tokens;
  // Require every component: some old CLI schemas omit tool/thought counts.
  const input = sum(t.input, t.tool),
    output = sum(t.output, t.thoughts);
  if (sum(input, output) !== integer(t.total)) throw Error('gemini_total_mismatch');
  return {
    source: 'gemini-cli',
    agent: 'Gemini CLI',
    provider: 'google',
    model: m.model,
    responseId: m.id,
    sessionId,
    occurredAt: m.timestamp,
    method: 'native',
    sourceVersion: 'gemini-cli.TokensSummary.2026-10-29',
    coverage: 'agent_normalized_usage_zero_defaults_possible',
    inputTokens: input,
    outputTokens: output,
    cachedInputTokens: integer(t.cached),
    cacheWriteInputTokens: '0',
    reasoningOutputTokens: integer(t.thoughts),
  };
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
