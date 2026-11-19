<script lang="ts">
  import type { UnifiedAgentOption } from '../lib/agents';
  import type { ViewTab } from '../lib/types';
  import AgentIcon from './AgentIcon.svelte';

  interface Props {
    activeTab: ViewTab;
    selectedAgent: string;
    agents: UnifiedAgentOption[];
    onSelectTab: (tab: ViewTab) => void;
    onSelectAgent: (agentId: string) => void;
    onCloseMobile: () => void;
  }

  let { activeTab, selectedAgent, agents, onSelectTab, onSelectAgent, onCloseMobile }: Props = $props();
</script>

<aside class="sidebar" aria-label="Navegación principal">
  <div class="aside-head">
    <div class="brand">
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" aria-hidden="true">
        <path d="M5 17v-5M10 17V7M15 17v-8M20 17V5" />
      </svg>
      <span>Monitor</span>
    </div>
    <button
      type="button"
      class="icon-button"
      aria-label="Cerrar menú"
      onclick={onCloseMobile}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
        <path d="M18 6L6 18M6 6l12 12" />
      </svg>
    </button>
  </div>

  <div class="aside-scroll">
    <div class="nav-label">Vistas</div>
    <nav aria-label="Vistas del panel">
      <button
        type="button"
        class:active={activeTab === 'overview'}
        aria-current={activeTab === 'overview' ? 'page' : undefined}
        onclick={() => {
          onSelectTab('overview');
          onCloseMobile();
        }}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <rect x="3" y="3" width="7" height="7" rx="1.5" />
          <rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" />
          <rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
        <span>Vista general</span>
      </button>

      <button
        type="button"
        class:active={activeTab === 'history'}
        aria-current={activeTab === 'history' ? 'page' : undefined}
        onclick={() => {
          onSelectTab('history');
          onCloseMobile();
        }}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7v5l3 2" />
        </svg>
        <span>Historial</span>
      </button>

      <button
        type="button"
        class:active={activeTab === 'sources'}
        aria-current={activeTab === 'sources' ? 'page' : undefined}
        onclick={() => {
          onSelectTab('sources');
          onCloseMobile();
        }}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <circle cx="12" cy="12" r="3" />
          <path d="M12 3v3M12 18v3M3 12h3M18 12h3" />
        </svg>
        <span>Fuentes y lectores</span>
      </button>
    </nav>

    {#if agents.length > 0}
      <div class="nav-label">Agentes</div>
      <nav aria-label="Filtrar por agente">
        <button
          type="button"
          class:active={selectedAgent === ''}
          aria-pressed={selectedAgent === ''}
          onclick={() => {
            onSelectAgent('');
            onCloseMobile();
          }}
        >
          <span>Todos los agentes</span>
        </button>
        {#each agents as ag (ag.id)}
          <button
            type="button"
            class:active={selectedAgent === ag.id}
            aria-pressed={selectedAgent === ag.id}
            data-agent-id={ag.id}
            onclick={() => {
              onSelectAgent(selectedAgent === ag.id ? '' : ag.id);
              onCloseMobile();
            }}
          >
            <AgentIcon agentId={ag.id} iconUrl={ag.iconUrl} size={14} />
            <span>{ag.name}</span>
          </button>
        {/each}
      </nav>
    {/if}
  </div>
</aside>
