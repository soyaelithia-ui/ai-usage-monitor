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
    iconUrl: agent.iconUrl,
  }));
  const tones = ['mint', 'blue', 'violet', 'amber', 'rose', 'cyan'];
  const extras = names
    .filter((name) => !known.has(name))
    .map((name) => {
      const id =
        name
          .toLowerCase()
          .replace(/[^a-z0-9_-]/g, '-')
          .replace(/^-+|-+$/g, '') || 'custom';
      const hashVal = [...name].reduce((acc, c) => acc + c.charCodeAt(0), 0);
      const tone = tones[hashVal % tones.length];
      return { name, source: id, tone, symbol: name.slice(0, 1).toUpperCase() };
    });
  return [...declared, ...extras];
};
export function sourceState(source) {
  if (!source) return { label: 'Métricas recibidas por ingesta', state: 'waiting' };
  return {
    state: source.state,
    label:
      {
        running: 'Lectura activa · tiempo real',
        waiting: 'Esperando registros',
        unsupported: 'Formato pendiente de verificar',
        error: 'Error de lectura',
        incomplete: 'Esperando escritura completa',
        starting: 'Iniciando lectura',
      }[source.state] ?? 'Sin información de la fuente',
  };
}

export function CodexIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M22.28 10.45a5.55 5.55 0 0 0-.47-4.49 5.7 5.7 0 0 0-4.04-2.77 5.78 5.78 0 0 0-5.18 1.15A5.67 5.67 0 0 0 8.35 3.5 5.75 5.75 0 0 0 4.1 6.13a5.58 5.58 0 0 0-.91 4.43 5.68 5.68 0 0 0 1.25 4.88 5.64 5.64 0 0 0 .47 4.49 5.72 5.72 0 0 0 4.04 2.77 5.8 5.8 0 0 0 5.18-1.15 5.67 5.67 0 0 0 4.24.84 5.74 5.74 0 0 0 4.25-2.63 5.59 5.59 0 0 0 .91-4.43 5.7 5.7 0 0 0-1.25-4.88zm-8.87 9.87a4.2 4.2 0 0 1-2.48-.48l3.65-2.11a.75.75 0 0 0 .38-.65v-5.17l1.55.9a.7.7 0 0 1 .37.64v4.38a4.23 4.23 0 0 1-3.47 2.49zm-8.4-4.86a4.13 4.13 0 0 1-.48-2.47l3.65 2.11a.75.75 0 0 0 .75 0l4.48-2.59v1.79a.74.74 0 0 1-.37.65l-3.8 2.19a4.23 4.23 0 0 1-4.23-1.68zm-1.05-8.4a4.17 4.17 0 0 1 2-2l3.65 2.11a.75.75 0 0 0 .37.65v5.17l-1.55-.9a.74.74 0 0 1-.37-.65V7.95a4.23 4.23 0 0 1-4.1-1.89zm14.73 3.65-4.48 2.59v-1.79a.75.75 0 0 1 .37-.65l3.8-2.19a4.23 4.23 0 0 1 4.23 1.68 4.15 4.15 0 0 1 .48 2.47l-3.65-2.11a.75.75 0 0 0-.75 0zm2.25 4.41a4.17 4.17 0 0 1-2 2l-3.65-2.11a.75.75 0 0 0-.37-.65V9.11l1.55.9a.75.75 0 0 1 .37.65v4.38a4.21 4.21 0 0 1 .1 1.08zm-9.52-1.92 2-1.15 2 1.15v2.31l-2 1.16-2-1.16v-2.31z" />
    </svg>
  );
}

export function OpenCodeIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M16 6H8v12h8V6zm4 16H4V2h16v20z" />
    </svg>
  );
}

export function AntigravityIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M21.751 22.607c1.34 1.005 3.35.335 1.508-1.508C17.73 15.74 18.904 1 12.037 1 5.17 1 6.342 15.74.815 21.1c-2.01 2.009.167 2.511 1.507 1.506 5.192-3.517 4.857-9.714 9.715-9.714 4.857 0 4.522 6.197 9.714 9.715z" />
    </svg>
  );
}

export function ClaudeIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.79-.048-2.698-.073-2.339-.097-2.266-.122-.571-.121L0 11.784l.055-.352.48-.321.686.06 1.52.103 2.278.158 1.652.097 2.449.255h.389l.055-.157-.134-.098-.103-.097-2.358-1.596-2.552-1.688-1.336-.972-.724-.491-.364-.462-.158-1.008.656-.722.881.06.225.061.893.686 1.908 1.476 2.491 1.833.365.304.145-.103.019-.073-.164-.274-1.355-2.446-1.446-2.49-.644-1.032-.17-.619a2.97 2.97 0 01-.104-.729L6.283.134 6.696 0l.996.134.42.364.62 1.414 1.002 2.229 1.555 3.03.456.898.243.832.091.255h.158V9.01l.128-1.706.237-2.095.23-2.695.08-.76.376-.91.747-.492.584.28.48.685-.067.444-.286 1.851-.559 2.903-.364 1.942h.212l.243-.242.985-1.306 1.652-2.064.73-.82.85-.904.547-.431h1.033l.76 1.129-.34 1.166-1.064 1.347-.881 1.142-1.264 1.7-.79 1.36.073.11.188-.02 2.856-.606 1.543-.28 1.841-.315.833.388.091.395-.328.807-1.969.486-2.309.462-3.439.813-.042.03.049.061 1.549.146.662.036h1.622l3.02.225.79.522.474.638-.079.485-1.215.62-1.64-.389-3.829-.91-1.312-.329h-.182v.11l1.093 1.068 2.006 1.81 2.509 2.33.127.578-.322.455-.34-.049-2.205-1.657-.851-.747-1.926-1.62h-.128v.17l.444.649 2.345 3.521.122 1.08-.17.353-.608.213-.668-.122-1.374-1.925-1.415-2.167-1.143-1.943-.14.08-.674 7.254-.316.37-.729.28-.607-.461-.322-.747.322-1.476.389-1.924.315-1.53.286-1.9.17-.632-.012-.042-.14.018-1.434 1.967-2.18 2.945-1.726 1.845-.414.164-.717-.37.067-.662.401-.589 2.388-3.036 1.44-1.882.93-1.086-.006-.158h-.055L4.132 18.56l-1.13.146-.487-.456.061-.746.231-.243 1.908-1.312-.006.006z" />
    </svg>
  );
}

export function GeminiIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z" />
    </svg>
  );
}

export function CursorIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M22.106 5.68L12.5.135a.998.998 0 00-.998 0L1.893 5.68a.84.84 0 00-.419.726v11.186c0 .3.16.577.42.727l9.607 5.547a.999.999 0 00.998 0l9.608-5.547a.84.84 0 00.42-.727V6.407a.84.84 0 00-.42-.726zm-.603 1.176L12.228 22.92c-.063.108-.228.064-.228-.061V12.34a.59.59 0 00-.295-.51l-9.11-5.26c-.107-.062-.063-.228.062-.228h18.55c.264 0 .428.286.296.514z" />
    </svg>
  );
}

export function DeepSeekIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M23.748 4.482c-.254-.124-.364.113-.512.234-.051.039-.094.09-.137.136-.372.397-.806.657-1.373.626-.829-.046-1.537.214-2.163.848-.133-.782-.575-1.248-1.247-1.548-.352-.156-.708-.311-.955-.65-.172-.241-.219-.51-.305-.774-.055-.16-.11-.323-.293-.35-.2-.031-.278.136-.356.276-.313.572-.434 1.202-.422 1.84.027 1.436.633 2.58 1.838 3.393.137.093.172.187.129.323-.082.28-.18.552-.266.833-.055.179-.137.217-.329.14a5.526 5.526 0 01-1.736-1.18c-.857-.828-1.631-1.742-2.597-2.458a11.365 11.365 0 00-.689-.471c-.985-.957.13-1.743.388-1.836.27-.098.093-.432-.779-.428-.872.004-1.67.295-2.687.684a3.055 3.055 0 01-.465.137 9.597 9.597 0 00-2.883-.102c-1.885.21-3.39 1.102-4.497 2.623C.082 8.606-.231 10.684.152 12.85c.403 2.284 1.569 4.175 3.36 5.653 1.858 1.533 3.997 2.284 6.438 2.14 1.482-.085 3.133-.284 4.994-1.86.47.234.962.327 1.78.397.63.059 1.236-.03 1.705-.128.735-.156.684-.837.419-.961-2.155-1.004-1.682-.595-2.113-.926 1.096-1.296 2.746-2.642 3.392-7.003.05-.347.007-.565 0-.845-.004-.17.035-.237.23-.256a4.173 4.173 0 001.545-.475c1.396-.763 1.96-2.015 2.093-3.517.02-.23-.004-.467-.247-.588z" />
    </svg>
  );
}

export function CopilotIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M16.295 5.29l-0.242 0.986h-2.32c-1.721 0-3.22 1.197-3.638 2.902l-2.101 8.593H3.798c-2.44 0-4.229-2.342-3.637-4.761l2.106-8.612c0.557-2.274 2.556-3.87 4.85-3.87h5.54c2.44 0 4.23 2.343 3.638 4.761M7.752 18.758l0.242-0.986h2.321c1.72 0 3.22-1.197 3.637-2.903l2.102-8.593h4.196c2.439 0 4.228 2.343 3.637 4.762l-2.107 8.611c-0.556 2.274-2.555 3.87-4.85 3.87h-5.54c-2.44 0-4.229-2.342-3.638-4.761" />
    </svg>
  );
}

export function GenericAgentIcon({ className = '', size = 16 }) {
  return (
    <svg
      className={`agent-svg-icon ${className}`}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="4" y="6" width="16" height="14" rx="3" fill="currentColor" fillOpacity="0.15" />
      <circle cx="9" cy="12" r="1.5" fill="currentColor" />
      <circle cx="15" cy="12" r="1.5" fill="currentColor" />
      <path d="M9 16h6" />
      <line x1="12" y1="2" x2="12" y2="6" />
      <circle cx="12" cy="2" r="1.2" fill="currentColor" />
    </svg>
  );
}

export function AgentIcon({ agent, size = 16 }) {
  if (agent?.iconUrl) {
    return (
      <img
        src={agent.iconUrl}
        alt=""
        className="agent-img-icon"
        width={size}
        height={size}
        onError={(e) => {
          e.currentTarget.style.display = 'none';
        }}
      />
    );
  }
  const name = (agent?.name || agent?.id || '').toLowerCase();
  if (name.includes('codex') || name.includes('openai') || name.includes('gpt') || name.includes('chatgpt')) {
    return <CodexIcon size={size} />;
  }
  if (name.includes('opencode')) return <OpenCodeIcon size={size} />;
  if (name.includes('antigravity')) return <AntigravityIcon size={size} />;
  if (name.includes('claude') || name.includes('anthropic')) return <ClaudeIcon size={size} />;
  if (name.includes('gemini') || name.includes('google')) return <GeminiIcon size={size} />;
  if (name.includes('cursor')) return <CursorIcon size={size} />;
  if (name.includes('deepseek')) return <DeepSeekIcon size={size} />;
  if (name.includes('copilot') || name.includes('github')) return <CopilotIcon size={size} />;
  return <GenericAgentIcon size={size} />;
}

export function AgentMark({ agent }) {
  return (
    <span className={`agent-mark agent-${agent?.tone || 'mint'}`} aria-hidden="true">
      <AgentIcon agent={agent} />
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
