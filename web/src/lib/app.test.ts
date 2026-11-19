import { describe, expect, it, beforeEach } from 'vitest';
import { formatCompact, formatExact, formatMoney, pctRatio } from './format';
import { THEMES, themeStore } from './theme.svelte';
import { buildFilterQuery } from './api';
import { buildAgentOptions, normalizeAgentId, resolveAgentMeta } from './agents';
import { requiredEndpointsForTab } from './viewLoader';

describe('formatters', () => {
  it('formats huge integers and money precisely', () => {
    expect(formatExact('9007199254740993')).toContain('9');
    expect(formatCompact('1500')).toMatch(/1[.,]5/);
    expect(formatMoney(null)).toBe('—');
    expect(formatMoney('0.001665')).toBe('$0.0017');
    expect(formatMoney('0.001665', true)).toBe('$0.001665');
    expect(pctRatio('25', '100')).toBe(25);
    expect(pctRatio('0', '0')).toBe(0);
  });
});

describe('themeStore', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.removeAttribute('data-mode');
  });

  it('supports all 4 themes in dark and light modes', () => {
    expect(THEMES.map((t) => t.id)).toEqual([
      'bioluminescent',
      'cute-pastel-blue',
      'burned-paper',
      'vivid-yellow-green',
    ]);

    for (const theme of THEMES) {
      themeStore.setTheme(theme.id);
      expect(document.documentElement.getAttribute('data-theme')).toBe(theme.id);
      expect(localStorage.getItem('pulse_ui_theme')).toBe(theme.id);
    }

    const prevMode = themeStore.mode;
    themeStore.toggleMode();
    const expected = prevMode === 'dark' ? 'light' : 'dark';
    expect(document.documentElement.getAttribute('data-mode')).toBe(expected);
    expect(localStorage.getItem('pulse_ui_mode')).toBe(expected);
  });
});

describe('shared filter query builder', () => {
  it('encodes window, agent, model, provider, sessionId, and pagination', () => {
    const q = buildFilterQuery(
      { window: '24h', agent: 'codex', model: 'gpt-6-astra', provider: 'openai', sessionId: 's1' },
      { limit: 50, offset: 100 },
    );
    const params = new URLSearchParams(q);
    expect(params.get('window')).toBe('24h');
    expect(params.get('agent')).toBe('codex');
    expect(params.get('model')).toBe('gpt-6-astra');
    expect(params.get('provider')).toBe('openai');
    expect(params.get('sessionId')).toBe('s1');
    expect(params.get('limit')).toBe('50');
    expect(params.get('offset')).toBe('100');
  });
});

describe('requiredEndpointsForTab (active-view-only fetching)', () => {
  it('fetches only overview endpoints and avoids over-fetching on SSE/filter', () => {
    expect(Array.from(requiredEndpointsForTab('overview', 'init', false)).sort()).toEqual([
      'analytics',
      'sources',
    ]);
    expect(Array.from(requiredEndpointsForTab('overview', 'sse', true))).toEqual(['analytics']);
    expect(Array.from(requiredEndpointsForTab('overview', 'filter', true))).toEqual(['analytics']);
    expect(Array.from(requiredEndpointsForTab('overview', 'tab', true))).toEqual(['analytics']);
  });

  it('fetches only history endpoints on tab/filter/page/sse', () => {
    expect(Array.from(requiredEndpointsForTab('history', 'init', false)).sort()).toEqual([
      'events',
      'sources',
    ]);
    expect(Array.from(requiredEndpointsForTab('history', 'tab', true))).toEqual(['events']);
    expect(Array.from(requiredEndpointsForTab('history', 'filter', true))).toEqual(['events']);
    expect(Array.from(requiredEndpointsForTab('history', 'page', true))).toEqual(['events']);
    expect(Array.from(requiredEndpointsForTab('history', 'sse', true))).toEqual(['events']);
  });

  it('fetches only sources endpoints on sources tab', () => {
    expect(Array.from(requiredEndpointsForTab('sources', 'tab', true)).sort()).toEqual([
      'discovery',
      'reconciliation',
      'sources',
    ]);
    expect(Array.from(requiredEndpointsForTab('sources', 'sse', true)).sort()).toEqual([
      'discovery',
      'reconciliation',
      'sources',
    ]);
    expect(Array.from(requiredEndpointsForTab('sources', 'filter', true))).toEqual(['reconciliation']);
  });
});

describe('unified agent normalization & options', () => {
  it('unifies canonical agent ids across sources, panels, and option lists without duplicates', () => {
    expect(normalizeAgentId('  Hermes ')).toBe('hermes');

    const sources = [
      { id: 'codex', name: 'Codex', state: 'running', tone: 'mint', symbol: 'C', reader: 'jsonl' },
      { id: 'antigravity', name: 'Antigravity', state: 'idle', tone: 'violet', symbol: 'A', reader: 'http' },
    ];
    expect(resolveAgentMeta('Codex', sources)?.id).toBe('codex');
    expect(resolveAgentMeta('antigravity', sources)?.name).toBe('Antigravity');

    const options = buildAgentOptions(
      sources,
      ['codex', 'Antigravity', 'hermes'],
      [
        {
          id: 'codex',
          name: 'Codex',
          count: 1,
          inputTokens: '10',
          outputTokens: '5',
          cachedInputTokens: '0',
          cacheWriteInputTokens: '0',
          reasoningOutputTokens: '0',
          normalInputTokens: '10',
          visibleOutputTokens: '5',
          totalTokens: '15',
          costCacheReadUsd: '0',
          costCacheWriteUsd: '0',
          costVisibleOutputUsd: '0.005',
          costReasoningUsd: '0',
          costInputUsd: '0.005',
          costCacheUsd: '0',
          costOutputUsd: '0.005',
          costUsd: '0.01',
          missingCache: 0,
          missingReasoning: 0,
          unpriced: 0,
          lastEventAt: '2026-11-19T00:00:00Z',
          buckets: [],
        },
      ],
    );
    expect(options.map((o) => o.id)).toEqual(['codex', 'antigravity', 'hermes']);
    expect(options.find((o) => o.id === 'codex')?.name).toBe('Codex');
    expect(options.find((o) => o.id === 'antigravity')?.name).toBe('Antigravity');
  });
});
