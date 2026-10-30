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
export function AgentSections({ agents, data, sources, onSelect, connected }) {
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
        {agents.map((agent, index) => {
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
        })}
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
