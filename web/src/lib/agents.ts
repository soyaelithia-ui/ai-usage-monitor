import type { AgentPanel, SourceAgent } from './types';

export interface UnifiedAgentOption {
  id: string;
  name: string;
  tone?: string;
  iconUrl?: string;
}

export function normalizeAgentId(raw: string): string {
  return raw.trim().toLowerCase();
}

export function resolveAgentMeta(idOrName: string, sources: SourceAgent[]): SourceAgent | undefined {
  const norm = normalizeAgentId(idOrName);
  if (!norm) return undefined;
  return (
    sources.find((s) => normalizeAgentId(s.id) === norm) ??
    sources.find((s) => normalizeAgentId(s.name) === norm)
  );
}

/**
 * Builds one unified, canonical-ID-keyed list of agents shared across Sidebar,
 * OverviewView, and HistoryView so selecting an agent anywhere keeps every view in sync.
 */
export function buildAgentOptions(
  sources: SourceAgent[],
  optionIds: string[] = [],
  panels: AgentPanel[] = [],
): UnifiedAgentOption[] {
  const byId = new Map<string, UnifiedAgentOption>();

  for (const s of sources) {
    const id = normalizeAgentId(s.id);
    if (!id) continue;
    byId.set(id, {
      id,
      name: s.name || s.id,
      tone: s.tone,
      iconUrl: s.iconUrl,
    });
  }

  for (const p of panels) {
    const id = normalizeAgentId(p.id || p.name);
    if (!id) continue;
    if (!byId.has(id)) {
      byId.set(id, { id, name: p.name || id });
    }
  }

  for (const rawOpt of optionIds) {
    const id = normalizeAgentId(rawOpt);
    if (!id) continue;
    if (!byId.has(id)) {
      const meta = resolveAgentMeta(rawOpt, sources);
      if (meta) {
        const metaId = normalizeAgentId(meta.id);
        if (!byId.has(metaId)) {
          byId.set(metaId, { id: metaId, name: meta.name, tone: meta.tone, iconUrl: meta.iconUrl });
        }
      } else {
        byId.set(id, { id, name: rawOpt });
      }
    }
  }

  return Array.from(byId.values());
}
