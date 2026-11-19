<script lang="ts">
  import { formatCompact, formatExact, formatMoney, pctRatio } from '../lib/format';
  import type { SummaryMetrics } from '../lib/types';

  interface Props {
    totals: SummaryMetrics;
  }

  let { totals }: Props = $props();

  const cachePct = $derived(pctRatio(totals.cachedInputTokens, totals.inputTokens));
  const reasonPct = $derived(pctRatio(totals.reasoningOutputTokens, totals.outputTokens));
</script>

<section class="stats" aria-label="Indicadores principales">
  <div class="stat">
    <span>Tokens totales</span>
    <strong class="tabular-nums">{formatCompact(totals.totalTokens)}</strong>
    <small class="tabular-nums">{formatExact(totals.totalTokens)} · {totals.count} eventos</small>
  </div>

  <div class="stat">
    <span>Entrada</span>
    <strong class="tabular-nums">{formatCompact(totals.inputTokens)}</strong>
    <small class="tabular-nums">Normal: {formatCompact(totals.normalInputTokens)} · Caché: {formatCompact(totals.cachedInputTokens)}</small>
  </div>

  <div class="stat">
    <span>Salida</span>
    <strong class="tabular-nums">{formatCompact(totals.outputTokens)}</strong>
    <small class="tabular-nums">Visible: {formatCompact(totals.visibleOutputTokens)} · Raz.: {formatCompact(totals.reasoningOutputTokens)}</small>
  </div>

  <div class="stat cost">
    <span>Coste estimado</span>
    <strong class="tabular-nums">{formatMoney(totals.costUsd)}</strong>
    <small class="tabular-nums">
      {#if totals.unpriced > 0}
        {totals.unpriced} sin tarifa verificada
      {:else}
        Equivalente API Standard
      {/if}
    </small>
  </div>
</section>

<section class="telemetry-bar-kpis" aria-label="Eficiencia de caché y razonamiento">
  <div class="telemetry-kpi-item">
    <div class="kpi-head">
      <span class="kpi-label">Ratio de caché leída</span>
      <span class="kpi-num mint-text tabular-nums">{cachePct.toFixed(1)}%</span>
    </div>
    <div class="mini-progress-bar" role="progressbar" aria-valuenow={Math.round(cachePct)} aria-valuemin={0} aria-valuemax={100}>
      <svg class="progress-svg" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true">
        <rect class="mint-svg" x="0" y="0" width={cachePct.toFixed(1)} height="4" />
      </svg>
    </div>
  </div>

  <div class="telemetry-kpi-item">
    <div class="kpi-head">
      <span class="kpi-label">Ratio de razonamiento</span>
      <span class="kpi-num violet-text tabular-nums">{reasonPct.toFixed(1)}%</span>
    </div>
    <div class="mini-progress-bar" role="progressbar" aria-valuenow={Math.round(reasonPct)} aria-valuemin={0} aria-valuemax={100}>
      <svg class="progress-svg" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true">
        <rect class="violet-svg" x="0" y="0" width={reasonPct.toFixed(1)} height="4" />
      </svg>
    </div>
  </div>
</section>
