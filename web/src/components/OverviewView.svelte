<script lang="ts">
  import { buildAgentOptions, resolveAgentMeta } from '../lib/agents';
  import { formatCompact, formatExact, formatMoney, formatShortTime } from '../lib/format';
  import type { AnalyticsResponse, SharedFilters, SourceAgent } from '../lib/types';
  import AgentIcon from './AgentIcon.svelte';
  import StatsRow from './StatsRow.svelte';

  interface Props {
    data: AnalyticsResponse | null;
    sources: SourceAgent[];
    filters: SharedFilters;
    onFilterChange: (patch: Partial<SharedFilters>) => void;
  }

  let { data, sources, filters, onFilterChange }: Props = $props();

  let selectedBucket = $state<number | null>(null);

  const buckets = $derived(data?.buckets ?? []);
  const agentOptions = $derived(
    buildAgentOptions(sources, data?.options?.agents ?? [], data?.agentPanels ?? []),
  );
  const maxTokens = $derived(
    Math.max(1, ...buckets.map((b) => Math.max(Number(b.inputTokens), Number(b.outputTokens)))),
  );
  const maxCost = $derived(
    Math.max(0.0001, ...buckets.map((b) => Number(b.cumulativeCostUsd ?? b.costUsd ?? 0))),
  );
  const maxModelTokens = $derived(
    Math.max(1, ...(data?.models ?? []).map((m) => Number(m.totalTokens))),
  );
  const hasActiveFilters = $derived(
    Boolean(filters.agent || filters.model || filters.provider || filters.sessionId),
  );

  function sparkPoints(panel: AnalyticsResponse['agentPanels'][number]): string {
    const bs = panel.buckets ?? [];
    const mx = Math.max(1, ...bs.map((b) => Number(b.inputTokens) + Number(b.outputTokens)));
    return bs
      .map((b, i) => {
        const x = (i / Math.max(1, bs.length - 1)) * 300;
        const y = 36 - ((Number(b.inputTokens) + Number(b.outputTokens)) / mx) * 32;
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');
  }
</script>

<div class="filters-bar" aria-label="Filtros compartidos">
  <select
    class="filter-select"
    aria-label="Filtrar por agente"
    value={filters.agent}
    onchange={(e) => onFilterChange({ agent: (e.currentTarget as HTMLSelectElement).value })}
  >
    <option value="">Todos los agentes</option>
    {#each agentOptions as ag (ag.id)}
      <option value={ag.id}>{ag.name}</option>
    {/each}
  </select>

  <select
    class="filter-select"
    aria-label="Filtrar por modelo"
    value={filters.model}
    onchange={(e) => onFilterChange({ model: (e.currentTarget as HTMLSelectElement).value })}
  >
    <option value="">Todos los modelos</option>
    {#each data?.options?.models ?? [] as m}
      <option value={m}>{m}</option>
    {/each}
  </select>

  <select
    class="filter-select"
    aria-label="Filtrar por proveedor"
    value={filters.provider}
    onchange={(e) => onFilterChange({ provider: (e.currentTarget as HTMLSelectElement).value })}
  >
    <option value="">Todos los proveedores</option>
    {#each data?.options?.providers ?? [] as p}
      <option value={p}>{p}</option>
    {/each}
  </select>

  <select
    class="filter-select"
    aria-label="Filtrar por sesión"
    value={filters.sessionId}
    onchange={(e) => onFilterChange({ sessionId: (e.currentTarget as HTMLSelectElement).value })}
  >
    <option value="">Todas las sesiones</option>
    {#each data?.options?.sessions ?? [] as s}
      <option value={s}>{s}</option>
    {/each}
  </select>

  {#if hasActiveFilters}
    <button
      type="button"
      onclick={() => onFilterChange({ agent: '', model: '', provider: '', sessionId: '' })}
    >
      Limpiar filtros
    </button>
  {/if}
</div>

{#if !data}
  <div class="notice">Cargando telemetría…</div>
{:else}
  {#if data.totals.count === 0}
    <div class="notice">
      No hay eventos registrados en la ventana seleccionada ({filters.window}). Cambia el rango temporal o envía telemetría a <code>/api/ingest</code>.
    </div>
  {/if}
  <StatsRow totals={data.totals} />

  <div class="charts-grid">
    <section class="panel" aria-labelledby="chart-activity-title">
      <div class="panel-heading">
        <h2 id="chart-activity-title">Actividad</h2>
        {#if selectedBucket !== null && buckets[selectedBucket]}
          <span class="chart-readout tabular-nums">
            {formatShortTime(buckets[selectedBucket].at ?? '')} ·
            {formatCompact(buckets[selectedBucket].inputTokens)} ↓ ·
            {formatCompact(buckets[selectedBucket].outputTokens)} ↑
          </span>
        {:else}
          <div class="legend">
            <span><i class="mint"></i>Entrada</span>
            <span><i class="violet"></i>Salida</span>
          </div>
        {/if}
      </div>
      <svg class="chart" viewBox="0 0 760 172" role="img" aria-label="Gráfica de tokens de entrada y salida">
        {#each [0, 0.5, 1] as f}
          <line
            x1="40"
            x2="740"
            y1={148 - f * 118}
            y2={148 - f * 118}
            stroke="var(--chart-grid)"
            stroke-dasharray={f === 0 ? undefined : '2 4'}
          />
          <text x="0" y={152 - f * 118} fill="var(--chart-text)" font-size="10">
            {formatCompact(Math.round(maxTokens * f))}
          </text>
        {/each}
        {#if selectedBucket !== null}
          <line
            x1={40 + selectedBucket * 11.7 + 4.4}
            x2={40 + selectedBucket * 11.7 + 4.4}
            y1="24"
            y2="148"
            stroke="var(--border-focus)"
            stroke-width="1"
            stroke-dasharray="2 2"
          />
        {/if}
        {#each buckets as b, i}
          {@const bx = 40 + i * 11.7}
          {@const inH = Math.max(0, (Number(b.inputTokens) / maxTokens) * 118)}
          {@const outH = Math.max(0, (Number(b.outputTokens) / maxTokens) * 118)}
          <g
            role="presentation"
            onmouseenter={() => { selectedBucket = i; }}
            onmouseleave={() => { selectedBucket = null; }}
          >
            <rect x={bx} y="24" width="11.7" height="124" fill="transparent" />
            <rect x={bx} y={148 - inH} width="4" height={inH} rx="1.5" fill="var(--chart-series-1)" />
            <rect x={bx + 4.8} y={148 - outH} width="4" height={outH} rx="1.5" fill="var(--chart-series-2)" />
          </g>
        {/each}
      </svg>
    </section>

    <section class="panel" aria-labelledby="chart-cost-title">
      <div class="panel-heading">
        <h2 id="chart-cost-title">Coste acumulado</h2>
        {#if selectedBucket !== null && buckets[selectedBucket]}
          <span class="chart-readout tabular-nums">
            {formatShortTime(buckets[selectedBucket].at ?? '')} · {formatMoney(buckets[selectedBucket].cumulativeCostUsd)}
          </span>
        {/if}
      </div>
      <svg class="chart" viewBox="0 0 760 172" role="img" aria-label="Gráfica de coste acumulado">
        {#each [0, 0.5, 1] as f}
          <line
            x1="40"
            x2="740"
            y1={148 - f * 118}
            y2={148 - f * 118}
            stroke="var(--chart-grid)"
            stroke-dasharray={f === 0 ? undefined : '2 4'}
          />
          <text x="0" y={152 - f * 118} fill="var(--chart-text)" font-size="10">
            {formatMoney(maxCost * f)}
          </text>
        {/each}
        {#if buckets.length > 0}
          <path
            d={`M40 148 ${buckets.map((b, i) => `L${(40 + i * 11.7).toFixed(1)} ${(148 - (Number(b.cumulativeCostUsd ?? 0) / maxCost) * 118).toFixed(1)}`).join(' ')} L730 148 Z`}
            fill="var(--chart-series-2)"
            opacity=".1"
          />
          <polyline
            points={buckets.map((b, i) => `${(40 + i * 11.7).toFixed(1)},${(148 - (Number(b.cumulativeCostUsd ?? 0) / maxCost) * 118).toFixed(1)}`).join(' ')}
            fill="none"
            stroke="var(--chart-series-2)"
            stroke-width="2"
          />
        {/if}
        {#if selectedBucket !== null}
          <line
            x1={40 + selectedBucket * 11.7 + 4.4}
            x2={40 + selectedBucket * 11.7 + 4.4}
            y1="24"
            y2="148"
            stroke="var(--border-focus)"
            stroke-width="1"
            stroke-dasharray="2 2"
          />
        {/if}
        {#each buckets as _, i}
          <rect
            role="presentation"
            x={40 + i * 11.7}
            y="24"
            width="11.7"
            height="124"
            fill="transparent"
            onmouseenter={() => { selectedBucket = i; }}
            onmouseleave={() => { selectedBucket = null; }}
          />
        {/each}
      </svg>
    </section>
  </div>

  <details class="models-disclosure" open>
    <summary class="models-disclosure-summary">
      <span>Desglose por modelo</span>
      <span class="models-count-badge tabular-nums">{data.models.length}</span>
    </summary>
    <div class="models-disclosure-body">
      {#if data.models.length === 0}
        <p class="empty-models-msg">Sin modelos en la ventana seleccionada.</p>
      {:else}
        {#each data.models as m}
          {@const ratio = Math.min(100, (Number(m.totalTokens) / maxModelTokens) * 100)}
          <div class="model-row">
            <strong>{m.name}</strong>
            <div class="model-bar-track" role="progressbar" aria-valuenow={Math.round(ratio)} aria-valuemin={0} aria-valuemax={100}>
              <svg class="progress-svg" viewBox="0 0 100 6" preserveAspectRatio="none" aria-hidden="true">
                <rect class="mint-svg" x="0" y="0" width={ratio.toFixed(1)} height="6" />
              </svg>
            </div>
            <span class="model-bar-val tabular-nums">
              {formatExact(m.totalTokens)} tok · {formatMoney(m.costUsd)}
            </span>
          </div>
        {/each}
      {/if}
    </div>
  </details>

  {#if data.agentPanels.length > 0}
    <section class="agent-cards" aria-label="Tarjetas de agentes">
      {#each data.agentPanels as panel (panel.id ?? panel.name)}
        {@const meta = resolveAgentMeta(panel.id ?? panel.name, sources)}
        {@const tone = meta?.tone || 'mint'}
        <article class="agent-card agent-{tone}">
          <div class="agent-card-head">
            <div class="agent-title">
              <span class="agent-mark">
                <AgentIcon agentId={meta?.id ?? panel.id ?? panel.name} iconUrl={meta?.iconUrl} size={15} />
              </span>
              <span>{panel.name}</span>
            </div>
            <span class="status-pill running tabular-nums">{panel.count} req</span>
          </div>
          <div class="agent-card-metrics">
            <div>
              <small>Tokens</small>
              <strong class="tabular-nums">{formatCompact(panel.totalTokens)}</strong>
            </div>
            <div>
              <small>Caché</small>
              <strong class="tabular-nums">{formatCompact(panel.cachedInputTokens)}</strong>
            </div>
            <div>
              <small>Coste</small>
              <strong class="tabular-nums">{formatMoney(panel.costUsd)}</strong>
            </div>
          </div>
          <svg class="agent-sparkline" viewBox="0 0 300 40" role="img" aria-label="Tendencia de tokens de {panel.name}">
            <line x1="0" x2="300" y1="36" y2="36" stroke="var(--chart-grid)" />
            <polyline
              points={sparkPoints(panel)}
              fill="none"
              stroke="var(--chart-series-1)"
              stroke-width="1.5"
            />
          </svg>
        </article>
      {/each}
    </section>
  {/if}

  {#if data.pricingNote}
    <p class="notice">{data.pricingNote}</p>
  {/if}
{/if}
