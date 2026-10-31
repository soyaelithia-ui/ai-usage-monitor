import { existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';

/**
 * Intelligent discovery of installed AI agents, runtimes, and log sources.
 * Scans standard system/user directories and active environment variables
 * without hardcoding fixed models or requiring manual server restarts.
 */
export function discoverLocalTechnologies(env = process.env, customPaths = {}) {
  const home = homedir();
  const xdgData = env.XDG_DATA_HOME || join(home, '.local', 'share');
  const xdgConfig = env.XDG_CONFIG_HOME || join(home, '.config');

  const candidates = [
    {
      id: 'opencode',
      name: 'OpenCode',
      reader: 'sqlite',
      tone: 'blue',
      symbol: 'O',
      paths: [
        customPaths.openCodeDb,
        env.OPENCODE_DB,
        join(xdgData, 'opencode', 'opencode.db'),
        join(home, '.opencode', 'opencode.db'),
      ].filter(Boolean),
      check: (p) => existsSync(p) && statSync(p).isFile(),
      query: {
        sql: "SELECT m.id,m.session_id,m.time_updated,m.data,s.version FROM session_message m JOIN session_v2 s ON s.id=m.session_id WHERE m.type IN ('assistant','compaction') AND (m.time_updated>? OR (m.time_updated=? AND m.id>?)) ORDER BY m.time_updated,m.id LIMIT 200",
        cursor: ['time_updated', 'id'],
        columns: {
          id: 'id',
          sessionId: 'session_id',
          updated: 'time_updated',
          version: 'version',
          payload: 'data',
        },
      },
      usage: {
        responseId: 'id',
        sessionId: 'context.sessionId',
        occurredAt: 'time.completed',
        occurredAtFallback: 'time.created',
        occurredAtFormat: 'epoch-ms',
        model: 'model.id',
        provider: 'model.providerID',
        inputTokens: ['tokens.input', 'tokens.cache.read', 'tokens.cache.write'],
        outputTokens: ['tokens.output', 'tokens.reasoning'],
        cachedInputTokens: 'tokens.cache.read',
        cacheWriteInputTokens: 'tokens.cache.write',
        reasoningOutputTokens: 'tokens.reasoning',
      },
    },
    {
      id: 'codex',
      name: 'Codex',
      reader: 'jsonl',
      tone: 'mint',
      symbol: 'C',
      paths: [
        customPaths.codexRoot,
        env.CODEX_ROOT,
        join(xdgData, 'codex'),
        join(home, '.codex'),
      ].filter(Boolean),
      check: (p) => existsSync(p) && statSync(p).isDirectory(),
      filePattern: '\\.jsonl(?:\\.[\\w-]+)?$',
      recordTypes: ['token_usage_record'],
      usage: {
        responseId: 'payload.response_id',
        sessionId: ['payload.session_id', 'payload.thread_id'],
        turnId: 'payload.turn_id',
        model: 'context.model',
        occurredAt: 'timestamp',
        inputTokens: 'payload.usage.input_tokens',
        outputTokens: 'payload.usage.output_tokens',
        cachedInputTokens: 'payload.usage.cached_input_tokens',
        cacheWriteInputTokens: 'payload.usage.cache_write_input_tokens',
        reasoningOutputTokens: 'payload.usage.reasoning_output_tokens',
      },
    },
    {
      id: 'antigravity',
      name: 'Antigravity',
      reader: 'contract',
      tone: 'violet',
      symbol: 'A',
      paths: [
        customPaths.antigravityRoot,
        env.ANTIGRAVITY_ROOT,
        join(home, '.gemini', 'antigravity-cli'),
        join(xdgData, 'antigravity'),
      ].filter(Boolean),
      check: (p) => existsSync(p),
      usage: {},
    },
  ];

  const results = [];
  for (const c of candidates) {
    const foundPath = c.paths.find((p) => {
      try {
        return c.check(p);
      } catch {
        return false;
      }
    });

    results.push({
      id: c.id,
      name: c.name,
      reader: c.reader,
      tone: c.tone,
      symbol: c.symbol,
      detected: !!foundPath,
      path: foundPath ?? null,
      config: foundPath ? { path: foundPath } : {},
      query: c.query,
      filePattern: c.filePattern,
      recordTypes: c.recordTypes,
      usage: c.usage,
    });
  }

  return results;
}
