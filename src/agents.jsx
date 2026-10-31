import React from 'react';

// Agent identity comes from the declarative catalogue served by /api/sources;
// configured agents stay reachable with zero usage, and unknown names from
// reported metrics still render with a neutral tone.
export const agentList = (names, catalog = []) => {
  const known = new Set(catalog.map((agent) => agent.name));
  const declared = catalog.map((agent) => ({
    name: agent.name,
    source: agent.id,
    tone: agent.tone,
    symbol: agent.symbol,
  }));
  const extras = names
    .filter((name) => !known.has(name))
    .map((name) => ({ name, source: null, tone: 'mint', symbol: name.slice(0, 1) }));
  return [...declared, ...extras];
};
export function sourceState(source) {
  if (!source) return { label: 'Métricas recibidas por ingesta', state: 'waiting' };
  return {
    state: source.state,
    label:
      {
        running: 'Lectura activa · cada 2 s',
        waiting: 'Esperando registros',
        unsupported: 'Formato pendiente de verificar',
        error: 'Error de lectura',
        incomplete: 'Esperando escritura completa',
        starting: 'Iniciando lectura',
      }[source.state] ?? 'Sin información de la fuente',
  };
}
export function AgentMark({ agent }) {
  return (
    <span className={`agent-mark agent-${agent.tone}`} aria-hidden="true">
      {agent.symbol}
    </span>
  );
}
export function AgentNavigation({ agents, selected, onSelect }) {
  return (
    <nav className="agent-navigation" aria-label="Apartados por agente">
      {agents.map((agent) => (
        <button
          key={agent.name}
          aria-label={`Apartado ${agent.name}`}
          aria-current={selected === agent.name ? 'page' : undefined}
          className={selected === agent.name ? 'active' : ''}
          onClick={() => onSelect(agent.name)}
          title={agent.name}
        >
          <AgentMark agent={agent} />
          <span className="agent-nav-name">{agent.name}</span>
        </button>
      ))}
    </nav>
  );
}
const exact = (value) => BigInt(value ?? 0).toLocaleString('es-ES');
const compact = (value) =>
  new Intl.NumberFormat('es-ES', { notation: 'compact', maximumFractionDigits: 1 }).format(
    Number(value ?? 0),
  );
const money = (value) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 4,
  }).format(Number(value ?? 0));
const date = (value) => new Date(value).toLocaleString('es-ES');
function Sparkline({ buckets = [] }) {
  const max = Math.max(
    1,
    ...buckets.flatMap((b) => [Number(b.inputTokens), Number(b.outputTokens)]),
  );
  const points = (key) =>
    buckets.map((b, i) => `${(i * 300) / 59},${53 - (Number(b[key]) / max) * 45}`).join(' ');
  return (
    <svg
      className="agent-sparkline"
      viewBox="0 0 300 60"
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <path d="M0 54H300" stroke="#2a3831" />
      {!!buckets.length && (
        <>
          <polyline
            points={points('inputTokens')}
            fill="none"
            stroke="#a5edc6"
            strokeWidth="1.8"
            vectorEffect="non-scaling-stroke"
          />
          <polyline
            points={points('outputTokens')}
            fill="none"
            stroke="#b59add"
            strokeWidth="1.8"
            vectorEffect="non-scaling-stroke"
          />
        </>
      )}
    </svg>
  );
}
export function AgentSections({ agents, data, sources, onSelect, connected, selected }) {
  const visibleAgents = agents.filter((agent) => {
    const metrics = data.agentPanels?.find((panel) => panel.name === agent.name);
    const hasActivity =
      Boolean(metrics) &&
      (Number(metrics.count || 0) > 0 ||
        Number(metrics.totalTokens || 0) > 0 ||
        Number(metrics.inputTokens || 0) > 0 ||
        Number(metrics.outputTokens || 0) > 0);
    const isSelected = Boolean(selected && selected === agent.name);
    return hasActivity || isSelected;
  });

  return (
    <section className="agent-sections reveal" aria-labelledby="agent-sections-title">
      <div className="agent-section-heading">
        <div>
          <h2 id="agent-sections-title">Tus agentes, por separado</h2>
          <p>Gráficas y métricas del periodo seleccionado</p>
        </div>
        <span className="agent-stream-state">
          {connected ? 'Actualización en vivo' : 'Esperando conexión'}
        </span>
      </div>
      <div className="agent-cards">
        {visibleAgents.length === 0 ? (
          <div
            className="agent-empty-notice"
            style={{
              gridColumn: '1 / -1',
              padding: '24px 16px',
              textAlign: 'center',
              color: '#889890',
              background: 'rgba(255, 255, 255, 0.02)',
              borderRadius: '12px',
              border: '1px dashed #2a3831',
            }}
          >
            <p style={{ margin: 0, fontSize: '0.92rem' }}>
              No hay agentes con actividad registrada en esta ventana de tiempo.
            </p>
          </div>
        ) : (
          visibleAgents.map((agent, index) => {
            const metrics = data.agentPanels?.find((panel) => panel.name === agent.name),
              status = sourceState(sources?.[agent.source]);
            return (
              <button
                className={`agent-card agent-${agent.tone}`}
                key={agent.name}
                style={{ animationDelay: `${Math.min(index, 7) * 60}ms` }}
                onClick={() => onSelect(agent.name)}
                aria-label={`Abrir apartado de ${agent.name}`}
              >
                <span className="agent-card-heading">
                  <AgentMark agent={agent} />
                  <strong>{agent.name}</strong>
                  <span className="agent-open" aria-hidden="true">
                    ↗
                  </span>
                </span>
                <span className={`agent-source-state state-${status.state}`}>
                  <i />
                  {status.label}
                </span>
                <span className="agent-card-metrics">
                  <span>
                    <small>Tokens</small>
                    <strong title={exact(metrics?.totalTokens)}>
                      {compact(metrics?.totalTokens)}
                    </strong>
                  </span>
                  <span>
                    <small>Respuestas</small>
                    <strong>{metrics?.count ?? 0}</strong>
                  </span>
                  <span>
                    <small>Equivalente API</small>
                    <strong>
                      {!metrics?.count
                        ? '—'
                        : metrics.count === metrics.unpriced
                          ? 'Sin estimar'
                          : money(metrics.costUsd)}
                    </strong>
                  </span>
                </span>
                <Sparkline buckets={metrics?.buckets} />
                <span className="agent-card-tokens">
                  <span>↓ {exact(metrics?.inputTokens)} entrada</span>
                  <span>↑ {exact(metrics?.outputTokens)} salida</span>
                </span>
                <span className="agent-card-foot">
                  {metrics?.lastEventAt
                    ? `Última respuesta: ${date(metrics.lastEventAt)}`
                    : 'Sin consumo registrado en esta ventana'}
                  <span aria-hidden="true">→</span>
                </span>
              </button>
            );
          })
        )}
      </div>
    </section>
  );
}
export function AgentStatus({ agent, data, source, connected }) {
  const status = sourceState(source),
    metrics = data.agentPanels?.find((panel) => panel.name === agent.name);
  return (
    <section
      className={`agent-status agent-${agent.tone} reveal`}
      aria-label={`Estado de ${agent.name}`}
    >
      <AgentMark agent={agent} />
      <div>
        <strong>{agent.name}</strong>
        <span className={`agent-source-state state-${status.state}`}>
          <i />
          {status.label}
        </span>
        {source?.reason && <p>{source.reason}</p>}
      </div>
      <div className="agent-status-time">
        <strong>{connected ? 'Panel en vivo' : 'Panel reconectando'}</strong>
        <span>
          {metrics?.lastEventAt
            ? `Último consumo en esta ventana: ${date(metrics.lastEventAt)}`
            : 'Esperando consumo en la ventana seleccionada'}
        </span>
        <small>
          {source?.lastScan
            ? `Fuente revisada: ${date(source.lastScan)}`
            : 'Se mostrarán métricas al recibir registros verificables'}
        </small>
      </div>
    </section>
  );
}
export function LiveTelemetry({ agent, metrics, source, connected, pulse }) {
  const buckets = metrics?.buckets || [];
  const latestBucket = buckets.length ? buckets[buckets.length - 1] : null;
  const currentTokens = latestBucket
    ? BigInt(latestBucket.inputTokens) + BigInt(latestBucket.outputTokens)
    : 0n;
  const speed = currentTokens > 0n ? Math.round(Number(currentTokens) / 60) : 0;
  const totalTurns = metrics?.count || 0;
  const cadence = totalTurns > 0 ? (totalTurns >= 5 ? '< 3s' : '~12s') : 'En espera';
  const status = sourceState(source);
  const isStreaming = connected && status.state === 'running';

  return (
    <section className={`live-telemetry agent-${agent?.tone || 'mint'} reveal`}>
      <div className="telemetry-header">
        <div className="telemetry-title">
          <div className={`telemetry-heartbeat ${isStreaming ? 'beating' : ''}`}>
            <svg viewBox="0 0 40 24" className="ecg-line" aria-hidden="true">
              <path d="M 0 12 L 10 12 L 14 3 L 18 21 L 22 9 L 26 15 L 30 12 L 40 12" />
            </svg>
            <span className="live-dot" />
          </div>
          <div>
            <h3>Telemetría en Tiempo Real</h3>
            <span className="telemetry-sub">
              {isStreaming ? 'Monitoreo activo de turnos y velocidad' : status.label}
            </span>
          </div>
        </div>
        <div className="telemetry-badges">
          <span className={`status-pill state-${status.state}`}>
            <i /> {status.label}
          </span>
          <span className="cadence-pill">
            ⏱ Cadencia: <strong>{cadence}</strong>
          </span>
        </div>
      </div>
      <div className="telemetry-grid">
        <div className="telemetry-kpi">
          <span className="kpi-label">Velocidad</span>
          <div className="kpi-value-row">
            <strong className="kpi-num animate-num">{speed.toLocaleString('es-ES')}</strong>
            <small className="kpi-unit">tokens / s</small>
          </div>
          <span className="kpi-hint">Ritmo estimado en la ventana activa</span>
        </div>
        <div className="telemetry-kpi">
          <span className="kpi-label">Entrada & Caché</span>
          <div className="kpi-value-row">
            <strong className="kpi-num mint-text">{compact(metrics?.inputTokens)}</strong>
            <small className="kpi-subnum">⚡ {compact(metrics?.cachedInputTokens)} en caché</small>
          </div>
          <div className="mini-progress-bar">
            <div
              className="bar-fill mint-fill"
              style={{
                width: `${Math.min(
                  100,
                  (Number(metrics?.cachedInputTokens || 0) /
                    Math.max(1, Number(metrics?.inputTokens || 1))) *
                    100,
                )}%`,
              }}
            />
          </div>
        </div>
        <div className="telemetry-kpi">
          <span className="kpi-label">Salida & Razonamiento</span>
          <div className="kpi-value-row">
            <strong className="kpi-num violet-text">{compact(metrics?.outputTokens)}</strong>
            <small className="kpi-subnum">
              🧠 {compact(metrics?.reasoningOutputTokens)} razonamiento
            </small>
          </div>
          <div className="mini-progress-bar">
            <div
              className="bar-fill violet-fill"
              style={{
                width: `${Math.min(
                  100,
                  (Number(metrics?.reasoningOutputTokens || 0) /
                    Math.max(1, Number(metrics?.outputTokens || 1))) *
                    100,
                )}%`,
              }}
            />
          </div>
        </div>
        <div className="telemetry-kpi">
          <span className="kpi-label">Respuestas & Coste Est.</span>
          <div className="kpi-value-row">
            <strong className="kpi-num">{metrics?.count ?? 0}</strong>
            <small className="kpi-subnum">
              {metrics?.costUsd ? money(metrics.costUsd) : 'Sin estimar'}
            </small>
          </div>
          <span className="kpi-hint">
            {metrics?.lastEventAt
              ? `Última: ${date(metrics.lastEventAt)}`
              : 'Sin actividad en este periodo'}
          </span>
        </div>
      </div>
      <div className="telemetry-stream-sparkline">
        <Sparkline buckets={buckets} />
      </div>
    </section>
  );
}
