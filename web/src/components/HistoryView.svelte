<script lang="ts">
  import { buildAgentOptions } from '../lib/agents';
  import { formatExact, formatFullTime, formatMoney, formatShortTime } from '../lib/format';
  import type { AnalyticsResponse, EventsResponse, SharedFilters, SourceAgent } from '../lib/types';

  interface Props {
    analytics: AnalyticsResponse | null;
    eventsPage: EventsResponse | null;
    sources: SourceAgent[];
    filters: SharedFilters;
    offset: number;
    limit: number;
    onFilterChange: (patch: Partial<SharedFilters>) => void;
    onPageChange: (nextOffset: number) => void;
  }

  let { analytics, eventsPage, sources, filters, offset, limit, onFilterChange, onPageChange }: Props = $props();

  const events = $derived(eventsPage?.events ?? []);
  const total = $derived(eventsPage?.total ?? 0);
  const options = $derived(eventsPage?.options ?? analytics?.options);
  const agentOptions = $derived(
    buildAgentOptions(sources, options?.agents ?? [], analytics?.agentPanels ?? []),
  );
  const canPrev = $derived(offset > 0);
  const canNext = $derived(offset + limit < total);
  const hasActiveFilters = $derived(
    Boolean(filters.agent || filters.model || filters.provider || filters.sessionId),
  );
</script>

<div class="filters-bar" aria-label="Filtros de historial">
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
    {#each options?.models ?? [] as m}
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
    {#each options?.providers ?? [] as p}
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
    {#each options?.sessions ?? [] as s}
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

<section class="panel" aria-labelledby="history-heading">
  <div class="panel-heading">
    <h2 id="history-heading">Historial de eventos</h2>
    <span class="models-count-badge tabular-nums">{total}</span>
  </div>

  {#if events.length === 0}
    <div class="empty-pad">
      No hay eventos registrados para los filtros seleccionados.
    </div>
  {:else}
    <div class="table-scroll">
      <ul class="history-list" aria-label="Eventos recientes">
        {#each events as ev (ev.id)}
          <li class="history-item">
            <details class="event-disclosure">
              <summary class="event-summary">
                <span class="tabular-nums muted-text">
                  <span class="event-chevron" aria-hidden="true">›</span>
                  {formatShortTime(ev.occurredAt)}
                </span>
                <span class="event-title-cell">
                  <strong>{ev.agent}</strong> · {ev.model}
                </span>
                <span class="tabular-nums event-num-cell">
                  {formatExact(ev.totalTokens)} tok
                </span>
                <span class="tabular-nums event-cost-cell">
                  {formatMoney(ev.costUsd, true)}
                </span>
              </summary>
              <dl class="event-detail">
                <div class="event-detail-item">
                  <dt>Fecha exacta</dt>
                  <dd>{formatFullTime(ev.occurredAt)}</dd>
                </div>
                <div class="event-detail-item">
                  <dt>Proveedor / Fuente</dt>
                  <dd>{ev.provider} · {ev.source}</dd>
                </div>
                <div class="event-detail-item">
                  <dt>Entrada / Caché leída / Escrita</dt>
                  <dd>
                    {formatExact(ev.inputTokens)} / {ev.cachedInputTokens ?? '—'} / {ev.cacheWriteInputTokens ?? '—'}
                  </dd>
                </div>
                <div class="event-detail-item">
                  <dt>Salida / Razonamiento</dt>
                  <dd>
                    {formatExact(ev.outputTokens)} / {ev.reasoningOutputTokens ?? '—'}
                  </dd>
                </div>
                <div class="event-detail-item">
                  <dt>ID de respuesta / Sesión</dt>
                  <dd>{ev.responseId} {ev.sessionId ? `· ${ev.sessionId}` : ''}</dd>
                </div>
                <div class="event-detail-item">
                  <dt>Nota de tarifa</dt>
                  <dd>{ev.costNote}</dd>
                </div>
              </dl>
            </details>
          </li>
        {/each}
      </ul>
    </div>

    <div class="pagination">
      <span class="tabular-nums">
        Mostrando {offset + 1}–{Math.min(offset + events.length, total)} de {total}
      </span>
      <div class="pagination-actions">
        <button type="button" disabled={!canPrev} onclick={() => onPageChange(Math.max(0, offset - limit))}>
          Anterior
        </button>
        <button type="button" disabled={!canNext} onclick={() => onPageChange(offset + limit)}>
          Siguiente
        </button>
      </div>
    </div>
  {/if}
</section>
