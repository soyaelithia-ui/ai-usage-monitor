<script lang="ts">
  interface Props {
    agentId: string;
    iconUrl?: string;
    size?: number;
  }

  let { agentId, iconUrl, size = 15 }: Props = $props();
  let imgFailed = $state(false);

  $effect(() => {
    iconUrl;
    imgFailed = false;
  });

  const normalized = $derived(agentId.toLowerCase());
</script>

{#if iconUrl && !imgFailed}
  <img
    src={iconUrl}
    alt=""
    width={size}
    height={size}
    class="agent-img-icon"
    loading="lazy"
    onerror={() => {
      imgFailed = true;
    }}
  />
{:else if normalized.includes('codex') || normalized.includes('openai')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
    <circle cx="12" cy="12" r="9" />
    <path d="M12 3v9l7.8 4.5M12 12L4.2 16.5M12 12l7.8-4.5" />
  </svg>
{:else if normalized.includes('opencode')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <rect x="4" y="4" width="16" height="16" rx="3" />
    <path d="M9 9h6v6H9z" />
  </svg>
{:else if normalized.includes('antigravity')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M12 4l7 15-7-3-7 3 7-15z" />
  </svg>
{:else if normalized.includes('claude')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
    <path d="M12 3v18M4.2 7.5l15.6 9M19.8 7.5l-15.6 9" />
  </svg>
{:else if normalized.includes('gemini')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" aria-hidden="true">
    <path d="M12 2l2.6 7.4L22 12l-7.4 2.6L12 22l-2.6-7.4L2 12l7.4-2.6z" />
  </svg>
{:else if normalized.includes('hermes')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
    <circle cx="12" cy="12" r="8" />
    <ellipse cx="12" cy="12" rx="11" ry="4" transform="rotate(-20 12 12)" />
  </svg>
{:else if normalized.includes('cursor')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" aria-hidden="true">
    <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9L12 3z" />
    <path d="M12 12l8-4.5M12 12v9M12 12L4 7.5" />
  </svg>
{:else if normalized.includes('copilot')}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M4 13a8 8 0 0 1 16 0v3a3 3 0 0 1-3 3h-2l-1-3h-4l-1 3H7a3 3 0 0 1-3-3v-3z" />
    <circle cx="9.5" cy="13" r="1.2" fill="currentColor" stroke="none" />
    <circle cx="14.5" cy="13" r="1.2" fill="currentColor" stroke="none" />
  </svg>
{:else}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <rect x="3" y="5" width="18" height="14" rx="3" />
    <path d="M8 10l3 2.5L8 15M13 15h4" />
  </svg>
{/if}
