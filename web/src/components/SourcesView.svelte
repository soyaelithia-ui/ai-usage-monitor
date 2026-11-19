<script lang="ts">
  import { formatExact, formatFullTime } from '../lib/format';
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

  const recentErrors = $derived(sources?.errors ?? analytics?.errors ?? []);
  const totalErrors = $derived(sources?.errorCount ?? analytics?.errorCount ?? recentErrors.length);

  function statusFor(id: string): CollectorStatus {
    const fromSources = sources?.[id];
    if (fromSources && typeof fromSources === 'object' && 'state' in fromSources) {
      return fromSources as CollectorStatus;
    }
    const fromAna = analytics?.collector?.[id];
    if (fromAna) return fromAna;
    return { state: 'waiting' };
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
