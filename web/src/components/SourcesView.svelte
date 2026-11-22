<script lang="ts">
  import { formatCompact, formatExact, formatFullTime, formatMoney } from '../lib/format';
  import type {
    AnalyticsResponse,
    CollectorStatus,
    DiscoveryResponse,
    ReconciliationResponse,
    SourcesResponse,
  } from '../lib/types';
  import AgentIcon from './AgentIcon.svelte';

  interface Props {
    sources: SourcesResponse | null;
    discovery: DiscoveryResponse | null;
    reconciliation: ReconciliationResponse | null;
    analytics: AnalyticsResponse | null;
  }

  let { sources, discovery, reconciliation, analytics }: Props = $props();

  let copiedHookId = $state('');

  const recentErrors = $derived(sources?.errors ?? analytics?.errors ?? []);
  const totalErrors = $derived(sources?.errorCount ?? analytics?.errorCount ?? recentErrors.length);
  const hooksOverview = $derived(sources?.hooks ?? null);

  function statusFor(id: string): CollectorStatus {
    const fromSources = sources?.[id];
    if (fromSources && typeof fromSources === 'object' && 'state' in fromSources) {
      return fromSources as CollectorStatus;
    }
    const fromAna = analytics?.collector?.[id];
    if (fromAna) return fromAna;
    return { state: 'waiting' };
  }

  async function copyInstallCmd(id: string, cmd: string): Promise<void> {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(cmd);
        copiedHookId = id;
        setTimeout(() => {
          if (copiedHookId === id) copiedHookId = '';
        }, 1800);
      }
    } catch {}
  }

  function quotaPctNumber(raw?: string): number {
    if (!raw) return 0;
    const n = Number(raw);
    if (!Number.isFinite(n) || n < 0) return 0;
    return Math.min(100, n);
  }
</script>

<section class="panel" aria-labelledby="readers-heading">
  <div class="panel-heading">
    <h2 id="readers-heading">Lectores y fuentes activas</h2>
    <span class="models-count-badge tabular-nums">{sources?.agents?.length ?? 0}</span>
  </div>

  <div class="panel-body-pad detected-grid">
    {#each sources?.agents ?? [] as ag (ag.id)}
      {@const st = statusFor(ag.id)}
      <div class="detected-card agent-{ag.tone || 'mint'}">
        <div class="source-row-head">
          <span class="agent-mark">
            <AgentIcon agentId={ag.id} iconUrl={ag.iconUrl} size={15} />
          </span>
          <div class="source-row-meta">
            <strong class="source-row-name">{ag.name}</strong>
            <small class="muted-text">
              Lector: {ag.reader}
              {#if st.files != null} · {st.files} archivos{/if}
            </small>
          </div>
        </div>
        <span class="status-pill" class:running={st.state === 'running'} class:error={st.state === 'error'}>
          {st.state}
        </span>
      </div>
    {/each}
  </div>
</section>

{#if hooksOverview}
  <section class="panel" aria-labelledby="hooks-heading">
    <div class="panel-heading">
      <h2 id="hooks-heading">Hooks nativos y cuotas en tiempo real</h2>
      <span class="models-count-badge tabular-nums">{hooksOverview.integrations.length}</span>
    </div>
    <div class="panel-body-pad">
      <p class="reconcile-note hooks-intro">
        Los hooks nativos muestran cuota y gasto en tiempo real dentro de cada agente sin interrumpir ni bloquear turnos.
        Ventana activa: <strong class="tabular-nums">{hooksOverview.window}</strong>
        {#if hooksOverview.globalQuota.limitUsd}
          · Cuota global: <strong class="tabular-nums">{formatMoney(hooksOverview.globalQuota.usedUsd)} / {formatMoney(hooksOverview.globalQuota.limitUsd)}</strong>
          ({hooksOverview.globalQuota.percentUsd}%)
        {:else}
          · Gasto global: <strong class="tabular-nums">{formatMoney(hooksOverview.globalQuota.usedUsd)}</strong>
          ({formatCompact(hooksOverview.globalQuota.usedTokens)} tok)
        {/if}
      </p>

      <div class="hooks-grid">
        {#each hooksOverview.integrations as hk (hk.id)}
          {@const qPct = quotaPctNumber(hk.quota.percentUsd || hk.quota.percentTokens)}
          <div class="hook-card">
            <div class="hook-card-head">
              <div class="source-row-head">
                <span class="agent-mark">
                  <AgentIcon agentId={hk.agentId || 'antigravity'} size={15} />
                </span>
                <div class="source-row-meta">
                  <strong class="source-row-name">{hk.name}</strong>
                  <small class="muted-text">{hk.channel}</small>
                </div>
              </div>
              <span
                class="status-pill"
                class:running={hk.installed}
                class:warn={hk.quota.state === 'warning'}
                class:error={hk.quota.state === 'exceeded'}
              >
                {#if hk.installed}
                  Instalado{hk.installScope ? ` (${hk.installScope})` : ''}
                {:else}
                  Disponible
                {/if}
              </span>
            </div>

            <div class="hook-meta-row tabular-nums">
              <span>Archivo: <code>{hk.workspaceConfig}</code></span>
              <span>
                {#if hk.quota.limitUsd}
                  {formatMoney(hk.quota.usedUsd)} / {formatMoney(hk.quota.limitUsd)} ({hk.quota.percentUsd}%)
                {:else if hk.quota.limitTokens}
                  {formatCompact(hk.quota.usedTokens)} / {formatCompact(hk.quota.limitTokens)} tok ({hk.quota.percentTokens}%)
                {:else}
                  {hk.quota.window}: {formatMoney(hk.quota.usedUsd)} · {formatCompact(hk.quota.usedTokens)} tok
                {/if}
              </span>
            </div>

            {#if hk.quota.limitUsd || hk.quota.limitTokens}
              <div
                class="mini-progress-bar"
                role="progressbar"
                aria-label={`Cuota ${hk.name}`}
                aria-valuenow={Math.round(qPct)}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <svg class="progress-svg" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true">
                  <rect
                    class:mint-svg={hk.quota.state === 'ok'}
                    class:warn-svg={hk.quota.state === 'warning'}
                    class:danger-svg={hk.quota.state === 'exceeded'}
                    x="0"
                    y="0"
                    width={qPct.toFixed(1)}
                    height="4"
                  />
                </svg>
              </div>
            {/if}

            <div class="hook-cmd-row">
              <code class="hook-cmd">{hk.installCommand}</code>
              <button
                type="button"
                class="hook-copy-btn"
                onclick={() => copyInstallCmd(hk.id, hk.installCommand)}
                aria-label={`Copiar comando para ${hk.name}`}
              >
                {copiedHookId === hk.id ? 'Copiado' : 'Copiar'}
              </button>
            </div>
          </div>
        {/each}
      </div>
    </div>
  </section>
{/if}

{#if discovery}
  <section class="panel" aria-labelledby="discovery-heading">
    <div class="panel-heading">
      <h2 id="discovery-heading">Tecnologías detectadas y modelos observados</h2>
      <span class="models-count-badge tabular-nums">{discovery.models.length}</span>
    </div>
    <div class="panel-body-pad">
      <div class="detected-grid grid-mb">
        {#each discovery.technologies as tech (tech.id)}
          <div class="detected-card agent-{tech.tone || 'mint'}">
            <div class="source-row-head">
              <span class="agent-mark">
                <AgentIcon agentId={tech.id} size={15} />
              </span>
              <div class="source-row-meta">
                <strong class="tech-row-name">{tech.name}</strong>
                <small class="muted-text">
                  {tech.reader} · {tech.configured ? 'Configurado' : tech.detected ? 'Detectado' : 'No detectado'}
                </small>
              </div>
            </div>
            <span class="status-pill" class:running={tech.detected}>
              {tech.detected ? 'Activo' : 'Inactivo'}
            </span>
          </div>
        {/each}
      </div>

      {#if discovery.models.length > 0}
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Modelo</th>
                <th>Estado de tarifa</th>
              </tr>
            </thead>
            <tbody>
              {#each discovery.models as m (m.model)}
                <tr>
                  <td>{m.model}</td>
                  <td>{m.priced ? 'Tarifa verificada' : 'Sin tarifa verificada'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>
  </section>
{/if}

{#if reconciliation}
  <section class="panel" aria-labelledby="reconcile-heading">
    <div class="panel-heading">
      <h2 id="reconcile-heading">Conciliación nativa</h2>
    </div>
    <div class="panel-body-pad">
      <div class="stats stats-mb">
        <div class="stat">
          <span>Respuestas</span>
          <strong class="tabular-nums">{formatExact(reconciliation.responses)}</strong>
        </div>
        <div class="stat">
          <span>Verificadas</span>
          <strong class="tabular-nums">{formatExact(reconciliation.matched)}</strong>
        </div>
        <div class="stat">
          <span>Sin verificación</span>
          <strong class="tabular-nums">{formatExact(reconciliation.unverified)}</strong>
        </div>
        <div class="stat">
          <span>Conflictos</span>
          <strong class="tabular-nums">{formatExact(reconciliation.conflicts)}</strong>
        </div>
      </div>
      <p class="reconcile-note">{reconciliation.note}</p>
    </div>
  </section>
{/if}

{#if recentErrors.length > 0}
  <section class="panel" aria-labelledby="errors-heading">
    <div class="panel-heading">
      <h2 id="errors-heading">Incidencias recientes</h2>
      <span class="models-count-badge tabular-nums">{totalErrors}</span>
    </div>
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Tipo</th>
            <th>Fecha</th>
          </tr>
        </thead>
        <tbody>
          {#each recentErrors as err}
            <tr>
              <td>{err.kind}</td>
              <td class="tabular-nums">{formatFullTime(err.at)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </section>
{/if}
