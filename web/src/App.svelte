<script lang="ts">
  import { onMount } from 'svelte';
  import AccessView from './components/AccessView.svelte';
  import Header from './components/Header.svelte';
  import HistoryView from './components/HistoryView.svelte';
  import OverviewView from './components/OverviewView.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import SourcesView from './components/SourcesView.svelte';
  import { buildAgentOptions, normalizeAgentId } from './lib/agents';
  import {
    checkSession,
    fetchAnalytics,
    fetchDiscovery,
    fetchEvents,
    fetchReconciliation,
    fetchSources,
    logoutSession,
    UnauthorizedError,
  } from './lib/api';
  import { connectTelemetryStream } from './lib/sse';
  import { themeStore } from './lib/theme.svelte';
  import type {
    AnalyticsResponse,
    DiscoveryResponse,
    EventsResponse,
    ReconciliationResponse,
    SharedFilters,
    SourcesResponse,
    ViewTab,
    WindowOption,
  } from './lib/types';
  import { requiredEndpointsForTab, type RefreshReason } from './lib/viewLoader';

  const PAGE_LIMIT = 50;

  let authChecking = $state(true);
  let authenticated = $state(false);
  let csrf = $state('');
  let sessionNotice = $state('');

  let activeTab = $state<ViewTab>('overview');
  let sidebarCollapsed = $state(false);
  let mobileDrawerOpen = $state(false);
  let sseLive = $state(false);

  let filters = $state<SharedFilters>({
    window: '1h',
    agent: '',
    model: '',
    provider: '',
    sessionId: '',
  });
  let offset = $state(0);

  let analytics = $state<AnalyticsResponse | null>(null);
  let eventsPage = $state<EventsResponse | null>(null);
  let sources = $state<SourcesResponse | null>(null);
  let discovery = $state<DiscoveryResponse | null>(null);
  let reconciliation = $state<ReconciliationResponse | null>(null);
  let loadError = $state('');

  let requestSeq = 0;

  const sidebarAgents = $derived(
    buildAgentOptions(
      sources?.agents ?? [],
      eventsPage?.options?.agents ?? analytics?.options?.agents ?? [],
      analytics?.agentPanels ?? [],
    ),
  );

  function expireToAccess(reason = 'Tu sesión ha caducado o fue revocada.'): void {
    requestSeq++;
    authenticated = false;
    csrf = '';
    sseLive = false;
    analytics = null;
    eventsPage = null;
    sources = null;
    discovery = null;
    reconciliation = null;
    sessionNotice = reason;
    if (typeof window !== 'undefined' && window.location.pathname !== '/access') {
      window.history.replaceState(null, '', '/access');
    }
  }

  async function refreshActiveView(reason: RefreshReason): Promise<void> {
    if (!authenticated) return;
    const seq = ++requestSeq;
    loadError = '';

    const tab = activeTab;
    const snapFilters = { ...filters };
    const snapOffset = offset;
    const needed = requiredEndpointsForTab(tab, reason, sources !== null);

    try {
      const tasks: Array<Promise<void>> = [];

      if (needed.has('analytics')) {
        tasks.push(
          fetchAnalytics(snapFilters).then((ana) => {
            if (!authenticated || seq !== requestSeq) return;
            analytics = ana;
          }),
        );
      }

      if (needed.has('events')) {
        tasks.push(
          fetchEvents(snapFilters, PAGE_LIMIT, snapOffset).then((evs) => {
            if (!authenticated || seq !== requestSeq) return;
            eventsPage = evs;
          }),
        );
      }

      if (needed.has('sources')) {
        tasks.push(
          fetchSources().then((srcs) => {
            if (!authenticated || seq !== requestSeq) return;
            sources = srcs;
          }),
        );
      }

      if (needed.has('discovery')) {
        tasks.push(
          fetchDiscovery().then((disc) => {
            if (!authenticated || seq !== requestSeq) return;
            discovery = disc;
          }),
        );
      }

      if (needed.has('reconciliation')) {
        tasks.push(
          fetchReconciliation(snapFilters).then((rec) => {
            if (!authenticated || seq !== requestSeq) return;
            reconciliation = rec;
          }),
        );
      }

      await Promise.all(tasks);
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        expireToAccess();
        return;
      }
      if (seq === requestSeq) {
        loadError = err instanceof Error ? err.message : 'Error al cargar la telemetría';
      }
    }
  }

  onMount(() => {
    themeStore.sync();
    void (async () => {
      try {
        const sess = await checkSession();
        if (sess) {
          csrf = sess.csrf;
          authenticated = true;
          if (window.location.pathname === '/access') {
            window.history.replaceState(null, '', '/');
          }
          void refreshActiveView('init');
        } else {
          authenticated = false;
        }
      } catch {
        authenticated = false;
      } finally {
        authChecking = false;
      }
    })();
  });

  $effect(() => {
    if (!authenticated) return;
    const stopStream = connectTelemetryStream({
      onStatus: (live) => {
        sseLive = live;
      },
      onChange: () => {
        void refreshActiveView('sse');
      },
      onExpired: () => {
        expireToAccess('Tu sesión ha caducado o fue revocada.');
      },
    });
    return () => {
      stopStream();
    };
  });

  function handleAuthenticated(nextCsrf: string, _expiresAt: string): void {
    csrf = nextCsrf;
    sessionNotice = '';
    authenticated = true;
    if (typeof window !== 'undefined') {
      window.history.replaceState(null, '', '/');
    }
    void refreshActiveView('init');
  }

  async function handleLogout(): Promise<void> {
    try {
      if (csrf) {
        await logoutSession(csrf);
      }
    } catch {}
    expireToAccess('Sesión cerrada.');
  }

  function handleSelectTab(nextTab: ViewTab): void {
    if (activeTab === nextTab) return;
    activeTab = nextTab;
    void refreshActiveView('tab');
  }

  function handleFilterChange(patch: Partial<SharedFilters>): void {
    const normalizedPatch: Partial<SharedFilters> = { ...patch };
    if (normalizedPatch.agent !== undefined) {
      normalizedPatch.agent = normalizeAgentId(normalizedPatch.agent);
    }
    filters = { ...filters, ...normalizedPatch };
    offset = 0;
    void refreshActiveView('filter');
  }

  function handleWindowChange(w: WindowOption): void {
    if (filters.window === w) return;
    filters = { ...filters, window: w };
    offset = 0;
    void refreshActiveView('filter');
  }

  function handlePageChange(nextOffset: number): void {
    offset = nextOffset;
    void refreshActiveView('page');
  }

  function handleToggleSidebar(): void {
    if (typeof window !== 'undefined' && window.innerWidth <= 900) {
      mobileDrawerOpen = !mobileDrawerOpen;
    } else {
      sidebarCollapsed = !sidebarCollapsed;
    }
  }
</script>

{#if authChecking}
  <div class="access-shell">
    <p class="muted-text">Comprobando sesión…</p>
  </div>
{:else if !authenticated}
  <AccessView initialNotice={sessionNotice} onAuthenticated={handleAuthenticated} />
{:else}
  <div
    class="app-shell"
    class:sidebar-collapsed={sidebarCollapsed}
    class:mobile-drawer-open={mobileDrawerOpen}
  >
    <div
      class="sidebar-backdrop"
      role="presentation"
      onclick={() => {
        mobileDrawerOpen = false;
      }}
    ></div>

    <Sidebar
      {activeTab}
      selectedAgent={filters.agent}
      agents={sidebarAgents}
      onSelectTab={handleSelectTab}
      onSelectAgent={(agId) => handleFilterChange({ agent: agId })}
      onCloseMobile={() => {
        mobileDrawerOpen = false;
      }}
    />

    <div class="main-wrap">
      <Header
        {activeTab}
        windowValue={filters.window}
        live={sseLive}
        onToggleSidebar={handleToggleSidebar}
        onWindowChange={handleWindowChange}
        onLogout={handleLogout}
      />

      <main class="workspace">
        <h1 class="sr-only">Panel operativo de AI Usage Monitor</h1>

        <div class="mobile-tabs" role="tablist" aria-label="Vistas móviles">
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'overview'}
            class:active={activeTab === 'overview'}
            onclick={() => handleSelectTab('overview')}
          >
            Resumen
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'history'}
            class:active={activeTab === 'history'}
            onclick={() => handleSelectTab('history')}
          >
            Historial
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === 'sources'}
            class:active={activeTab === 'sources'}
            onclick={() => handleSelectTab('sources')}
          >
            Fuentes
          </button>
        </div>

        {#if loadError}
          <div class="notice" role="alert">
            {loadError}
            <button type="button" class="inline-retry-btn" onclick={() => refreshActiveView('tab')}>
              Reintentar
            </button>
          </div>
        {/if}

        <div class="tab-content">
          {#if activeTab === 'overview'}
            <OverviewView
              data={analytics}
              sources={sources?.agents ?? []}
              {filters}
              onFilterChange={handleFilterChange}
            />
          {:else if activeTab === 'history'}
            <HistoryView
              {analytics}
              {eventsPage}
              sources={sources?.agents ?? []}
              {filters}
              {offset}
              limit={PAGE_LIMIT}
              onFilterChange={handleFilterChange}
              onPageChange={handlePageChange}
            />
          {:else}
            <SourcesView
              {sources}
              {discovery}
              {reconciliation}
              {analytics}
            />
          {/if}
        </div>
      </main>
    </div>
  </div>
{/if}
