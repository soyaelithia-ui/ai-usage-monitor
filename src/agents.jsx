import React, { useState, useEffect, useRef } from 'react';

export function AnimatedNumber({ value, format = (v) => v, duration = 500, className = '' }) {
  const numericTarget =
    value == null
      ? 0
      : typeof value === 'bigint'
        ? Number(value)
        : typeof value === 'number'
          ? value
          : Number(value) || 0;

  const [displayValue, setDisplayValue] = useState(numericTarget);
  const prevValueRef = useRef(numericTarget);
  const animFrameRef = useRef(null);
  const [isTick, setIsTick] = useState(false);

  useEffect(() => {
    const prefersReducedMotion =
      typeof window !== 'undefined' &&
      window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches;

    const start = prevValueRef.current;
    const end = numericTarget;
    prevValueRef.current = end;

    if (prefersReducedMotion || start === end || isNaN(end)) {
      setDisplayValue(end);
      return;
    }

    setIsTick(true);
    const tickTimer = setTimeout(() => setIsTick(false), 260);

    const startTime = performance.now();
    const diff = end - start;

    const animate = (now) => {
      const elapsed = now - startTime;
      const progress = Math.min(1, elapsed / duration);
      const ease = 1 - Math.pow(1 - progress, 3);
      const current = start + diff * ease;

      setDisplayValue(current);

      if (progress < 1) {
        animFrameRef.current = requestAnimationFrame(animate);
      } else {
        setDisplayValue(end);
      }
    };

    animFrameRef.current = requestAnimationFrame(animate);

    return () => {
      clearTimeout(tickTimer);
      if (animFrameRef.current) cancelAnimationFrame(animFrameRef.current);
    };
  }, [numericTarget, duration]);

  if (value == null) {
    return <span className={`animated-number ${className}`}>{format(null)}</span>;
  }

  return (
    <span className={`animated-number ${isTick ? 'number-tick' : ''} ${className}`}>
      {format(displayValue)}
    </span>
  );
}

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
    <span className={`agent-mark agent-${agent?.tone || 'mint'}`} aria-hidden="true">
      {agent?.symbol || (agent?.name ? agent.name.slice(0, 1).toUpperCase() : '◈')}
    </span>
  );
}
export function AgentNavigation({ agents, selected, onSelect }) {
  return (
    <nav className="agent-navigation" aria-label="Apartados por agente">
      {agents.map((agent) => (
        <button
          key={agent.name}
          type="button"
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
export const exact = (value) => {
  if (value == null || value === '') return '0';
  try {
    if (typeof value === 'bigint') return value.toLocaleString('es-ES');
    const n = Math.round(Number(value));
    return isNaN(n) ? String(value) : BigInt(n).toLocaleString('es-ES');
  } catch {
    return String(value);
  }
};
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
    ...buckets.flatMap((b) => [Number(b.inputTokens || 0), Number(b.outputTokens || 0)]),
  );
  const divisor = Math.max(1, buckets.length - 1);
  const points = (key) => {
    if (!buckets.length) return '';
    if (buckets.length === 1) {
      const y = 53 - (Number(buckets[0][key] || 0) / max) * 45;
      return `0,${y} 300,${y}`;
    }
    return buckets
      .map((b, i) => `${(i * 300) / divisor},${53 - (Number(b[key] || 0) / max) * 45}`)
      .join(' ');
  };
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
          <div className="agent-empty-notice">
            <p>No hay agentes con actividad registrada en esta ventana de tiempo.</p>
          </div>
        ) : (
          visibleAgents.map((agent, index) => {
            const metrics = data.agentPanels?.find((panel) => panel.name === agent.name),
              status = sourceState(sources?.[agent.source]);
            return (
              <button
                type="button"
                className={`agent-card agent-${agent.tone}`}
                key={agent.name}
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
                      <AnimatedNumber value={metrics?.totalTokens} format={compact} />
                    </strong>
                  </span>
                  <span>
                    <small>Respuestas</small>
                    <strong>
                      <AnimatedNumber
                        value={metrics?.count ?? 0}
                        format={(v) => Math.round(v).toLocaleString('es-ES')}
                      />
                    </strong>
                  </span>
                  <span>
                    <small>Equivalente API</small>
                    <strong>
                      {!metrics?.count ? (
                        '—'
                      ) : metrics.count === metrics.unpriced ? (
                        'Sin estimar'
                      ) : (
                        <AnimatedNumber value={metrics.costUsd} format={money} />
                      )}
                    </strong>
                  </span>
                </span>
                <Sparkline buckets={metrics?.buckets} />
                <span className="agent-card-tokens">
                  <span>
                    ↓ <AnimatedNumber value={metrics?.inputTokens} format={exact} /> entrada
                  </span>
                  <span>
                    ↑ <AnimatedNumber value={metrics?.outputTokens} format={exact} /> salida
                  </span>
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
export function LiveTelemetry({ agent, metrics, source, connected }) {
  const buckets = metrics?.buckets || [];
  const latestBucket = buckets.length ? buckets[buckets.length - 1] : null;
  const currentTokens = latestBucket
    ? BigInt(latestBucket.inputTokens || 0) + BigInt(latestBucket.outputTokens || 0)
    : 0n;
  const speed = currentTokens > 0n ? Math.round(Number(currentTokens) / 60) : 0;
  const totalTurns = metrics?.count || 0;
  const cadence = totalTurns > 0 ? (totalTurns >= 5 ? '< 3s' : '~12s') : 'En espera';
  const status = sourceState(source);
  const isStreaming = connected && status.state === 'running';

  const inputTotal = Math.max(1, Number(metrics?.inputTokens || 0));
  const cachedRatio = Math.min(
    100,
    Math.round((Number(metrics?.cachedInputTokens || 0) / inputTotal) * 100),
  );
  const outputTotal = Math.max(1, Number(metrics?.outputTokens || 0));
  const reasoningRatio = Math.min(
    100,
    Math.round((Number(metrics?.reasoningOutputTokens || 0) / outputTotal) * 100),
  );

  return (
    <div
      className={`live-telemetry-bar agent-${agent?.tone || 'mint'}`}
      aria-labelledby="telemetry-heading"
    >
      <div className="telemetry-bar-main">
        <div className="telemetry-title">
          <div className={`telemetry-heartbeat ${isStreaming ? 'beating' : ''}`}>
            <svg viewBox="0 0 40 24" className="ecg-line" aria-hidden="true">
              <path d="M 0 12 L 10 12 L 14 3 L 18 21 L 22 9 L 26 15 L 30 12 L 40 12" />
            </svg>
            <span className="live-dot" aria-hidden="true" />
          </div>
          <div>
            <h3 id="telemetry-heading">Telemetría en tiempo real</h3>
            <span className="telemetry-sub">
              {isStreaming ? 'Monitoreo activo de velocidad y turnos' : status.label}
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
      <div className="telemetry-bar-kpis">
        <div className="telemetry-kpi-item">
          <span className="kpi-label">Velocidad</span>
          <div className="kpi-val">
            <strong className="kpi-num">
              <AnimatedNumber value={speed} format={(v) => Math.round(v).toLocaleString('es-ES')} />
            </strong>
            <small>tokens / s</small>
          </div>
        </div>
        <div className="telemetry-kpi-item">
          <span className="kpi-label">Eficiencia de Caché</span>
          <div className="kpi-val">
            <strong className="kpi-num mint-text">
              <AnimatedNumber value={cachedRatio} format={(v) => `${Math.round(v)}%`} />
            </strong>
            <small>de entrada en caché</small>
          </div>
          <div
            className="mini-progress-bar"
            role="progressbar"
            aria-valuenow={cachedRatio}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label="Porcentaje de entrada en caché"
          >
            <div className="bar-fill mint-fill" style={{ width: `${cachedRatio}%` }} />
          </div>
        </div>
        <div className="telemetry-kpi-item">
          <span className="kpi-label">Razonamiento</span>
          <div className="kpi-val">
            <strong className="kpi-num violet-text">
              <AnimatedNumber value={reasoningRatio} format={(v) => `${Math.round(v)}%`} />
            </strong>
            <small>de salida en razonamiento</small>
          </div>
          <div
            className="mini-progress-bar"
            role="progressbar"
            aria-valuenow={reasoningRatio}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label="Porcentaje de razonamiento"
          >
            <div className="bar-fill violet-fill" style={{ width: `${reasoningRatio}%` }} />
          </div>
        </div>
      </div>
    </div>
  );
}
