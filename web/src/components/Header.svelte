<script lang="ts">
  import { THEMES, themeStore } from '../lib/theme.svelte';
  import type { ViewTab, WindowOption } from '../lib/types';

  interface Props {
    activeTab: ViewTab;
    windowValue: WindowOption;
    live: boolean;
    onToggleSidebar: () => void;
    onWindowChange: (w: WindowOption) => void;
    onLogout: () => void;
  }

  let { activeTab, windowValue, live, onToggleSidebar, onWindowChange, onLogout }: Props = $props();

  let themeMenuOpen = $state(false);
  let switcherEl: HTMLDivElement | undefined = $state();

  const windows: WindowOption[] = ['15m', '1h', '24h', '7d'];

  const title = $derived(
    activeTab === 'overview'
      ? 'Vista general'
      : activeTab === 'history'
        ? 'Historial de eventos'
        : 'Fuentes y lectores',
  );

  function selectTheme(id: (typeof THEMES)[number]['id']) {
    themeStore.setTheme(id);
    themeMenuOpen = false;
  }

  function handleWindowClick(e: MouseEvent) {
    if (themeMenuOpen && switcherEl && !switcherEl.contains(e.target as Node)) {
      themeMenuOpen = false;
    }
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && themeMenuOpen) {
      themeMenuOpen = false;
    }
  }
</script>

<svelte:window onclick={handleWindowClick} onkeydown={handleWindowKeydown} />

<header class="topbar">
  <div class="header-left">
    <button
      type="button"
      class="icon-button"
      aria-label="Alternar navegación"
      onclick={onToggleSidebar}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
        <path d="M4 7h16M4 12h16M4 17h16" />
      </svg>
    </button>
    <p class="top-title">
      <span>{title}</span>
      <span
        class="live-dot-inline"
        class:on={live}
        title={live ? 'SSE en directo conectado' : 'Reconectando SSE'}
        aria-label={live ? 'SSE conectado' : 'SSE desconectado'}
      ></span>
    </p>
  </div>

  <div class="header-right">
    <div class="segments" role="group" aria-label="Ventana temporal">
      {#each windows as w}
        <button
          type="button"
          class:selected={windowValue === w}
          aria-pressed={windowValue === w}
          onclick={() => onWindowChange(w)}
        >
          {w}
        </button>
      {/each}
    </div>

    <div class="theme-switcher" bind:this={switcherEl}>
      <button
        type="button"
        class="icon-button"
        aria-label="Seleccionar tema"
        aria-expanded={themeMenuOpen}
        onclick={() => {
          themeMenuOpen = !themeMenuOpen;
        }}
      >
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="13.5" cy="6.5" r=".5" fill="currentColor" />
          <circle cx="17.5" cy="10.5" r=".5" fill="currentColor" />
          <circle cx="8.5" cy="7.5" r=".5" fill="currentColor" />
          <circle cx="6.5" cy="12.5" r=".5" fill="currentColor" />
          <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z" />
        </svg>
      </button>

      {#if themeMenuOpen}
        <div class="theme-dropdown-menu" role="menu" aria-label="Temas disponibles">
          {#each THEMES as t}
            <button
              type="button"
              role="menuitemradio"
              aria-checked={themeStore.theme === t.id}
              class="theme-menu-item"
              class:active={themeStore.theme === t.id}
              onclick={() => selectTheme(t.id)}
            >
              <span>{t.label}</span>
              {#if themeStore.theme === t.id}
                <span aria-hidden="true">✓</span>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <button
      type="button"
      class="icon-button mode-toggle"
      aria-label={themeStore.mode === 'dark' ? 'Cambiar a modo claro' : 'Cambiar a modo oscuro'}
      title={themeStore.mode === 'dark' ? 'Modo claro' : 'Modo oscuro'}
      onclick={() => themeStore.toggleMode()}
    >
      {#if themeStore.mode === 'dark'}
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
        </svg>
      {:else}
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
          <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
        </svg>
      {/if}
    </button>

    <button
      type="button"
      class="icon-button"
      aria-label="Cerrar sesión"
      title="Cerrar sesión"
      onclick={onLogout}
    >
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
        <polyline points="16 17 21 12 16 7" />
        <line x1="21" y1="12" x2="9" y2="12" />
      </svg>
    </button>
  </div>
</header>
