import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import { agentList, AgentNavigation, AgentSections, AgentStatus } from './agents.jsx';
const exact = (x) => (x == null ? 'No disponible' : BigInt(x).toLocaleString('es-ES'));
const compact = (x) => {
  const n = Number(x);
  return new Intl.NumberFormat('es-ES', { notation: 'compact', maximumFractionDigits: 1 }).format(
    n,
  );
};
const money = (x) =>
  x == null
    ? 'Sin estimar'
    : new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        maximumFractionDigits: 4,
      }).format(Number(x));
const time = (x) => new Date(x).toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit' });
const fulltime = (x) => new Date(x).toLocaleString('es-ES');
function Icon({ type = 'activity' }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      {type === 'activity' ? (
        <path d="M2 13h4l3-8 5 15 3-7h5" />
      ) : type === 'grid' ? (
        <>
          <rect x="3" y="3" width="7" height="7" rx="1" />
          <rect x="14" y="3" width="7" height="7" rx="1" />
          <rect x="3" y="14" width="7" height="7" rx="1" />
          <rect x="14" y="14" width="7" height="7" rx="1" />
        </>
      ) : (
        <>
          <path d="M12 3v9m-5-6a8 8 0 1 0 10 0" />
        </>
      )}
    </svg>
  );
}
function Chart({ data, cost = false }) {
  const [selected, setSelected] = useState(null);
  const rows = data.buckets;
  const max = Math.max(
    1,
    ...rows.map((b) =>
      cost ? Number(b.cumulativeCostUsd) : Math.max(Number(b.inputTokens), Number(b.outputTokens)),
    ),
  );
  const x = (i) => 38 + i * 11.8,
    y = (v) => 160 - (Number(v) / max) * 125;
  const chosen = selected === null ? null : rows[selected];
  return (
    <section className="panel chart-panel">
      <div className="panel-heading">
        <div>
          <h2>{cost ? 'Coste acumulado' : 'El ritmo de tus tokens'}</h2>
          <p>
            {cost
              ? 'Equivalente API · USD · periodo seleccionado'
              : `Entrada y salida cada ${data.intervalMs / 60000 < 1 ? `${data.intervalMs / 1000} s` : `${data.intervalMs / 60000} min`}`}
          </p>
        </div>
        <span className="chart-symbol">{cost ? '↗' : '⌁'}</span>
      </div>
      {!cost && (
        <div className="legend">
          <span>
            <i className="mint" />
            Entrada
          </span>
          <span>
            <i className="violet" />
            Salida
          </span>
        </div>
      )}
      <svg
        className="chart"
        viewBox="0 0 760 190"
        role="img"
        aria-label={cost ? 'Gráfica de coste acumulado' : 'Gráfica de tokens de entrada y salida'}
      >
        {[0, 0.5, 1].map((f) => (
          <g key={f}>
            <line
              x1="38"
              x2="742"
              y1={160 - f * 125}
              y2={160 - f * 125}
              stroke="#26332e"
              strokeDasharray="3 5"
            />
            <text x="0" y={164 - f * 125} fill="#83958b" fontSize="10">
              {cost ? money(max * f) : compact(Math.round(max * f))}
            </text>
          </g>
        ))}
        {cost ? (
          <>
            <path
              d={`M38 160 ${rows.map((b, i) => `L${x(i)} ${y(b.cumulativeCostUsd)}`).join(' ')} L734 160 Z`}
              fill="#b6a2d6"
              opacity=".08"
            />
            <polyline
              points={rows.map((b, i) => `${x(i)},${y(b.cumulativeCostUsd)}`).join(' ')}
              fill="none"
              stroke="#c5abea"
              strokeWidth="2.5"
            />
          </>
        ) : (
          rows.map((b, i) => (
            <g key={b.at}>
              <rect
                x={x(i)}
                y={y(b.inputTokens)}
                width="4"
                height={160 - y(b.inputTokens)}
                fill="#a5edc6"
                rx="1"
              />
              <rect
                x={x(i) + 4.5}
                y={y(b.outputTokens)}
                width="4"
                height={160 - y(b.outputTokens)}
                fill="#ad91d6"
                rx="1"
              />
            </g>
          ))
        )}
        {selected !== null && (
          <line
            x1={x(selected)}
            x2={x(selected)}
            y1="25"
            y2="160"
            stroke="#eee"
            strokeDasharray="3 3"
          />
        )}
        {rows.map((b, i) => (
          <rect
            key={i}
            x={x(i) - 2}
            y="20"
            width="12"
            height="142"
            fill="transparent"
            onClick={() => setSelected(i)}
          >
            <title>
              {fulltime(b.at)} · Entrada {exact(b.inputTokens)} · Salida {exact(b.outputTokens)} ·
              USD {b.cumulativeCostUsd}
            </title>
          </rect>
        ))}
        <text x="38" y="184" fill="#83958b" fontSize="11">
          {time(data.from)}
        </text>
        <text x="365" y="184" fill="#83958b" fontSize="11">
          {time((data.from + data.to) / 2)}
        </text>
        <text x="706" y="184" fill="#83958b" fontSize="11">
          {time(data.to)}
        </text>
      </svg>
      <label className="chart-control">
        Explorar intervalos{' '}
        <input
          aria-label={cost ? 'Intervalo de coste' : 'Intervalo de tokens'}
          type="range"
          min="0"
          max="59"
          value={selected ?? 59}
          onChange={(e) => setSelected(Number(e.target.value))}
        />
      </label>
      <div className="chart-detail" aria-live="polite">
        {chosen ? (
          <>
            <span>{fulltime(chosen.at)}</span>
            {cost ? (
              <strong>
                USD {chosen.cumulativeCostUsd}{' '}
                <small>· {chosen.unpriced} sin estimar en este intervalo</small>
              </strong>
            ) : (
              <strong>
                ↓ {exact(chosen.inputTokens)} <small>entrada</small> &nbsp; ↑{' '}
                {exact(chosen.outputTokens)} <small>salida · {chosen.count} respuestas</small>
              </strong>
            )}
          </>
        ) : (
          <span>
            {data.totals.count
              ? 'Selecciona un punto o usa el control para ver cifras exactas.'
              : 'Sin actividad en esta ventana. Las nuevas respuestas aparecerán aquí.'}
          </span>
        )}
      </div>
    </section>
  );
}
function Distribution({ data }) {
  const [kind, setKind] = useState('models');
  const rows = data[kind];
  const max = Math.max(1, ...rows.map((r) => Number(r.totalTokens)));
  return (
    <section className="panel distribution">
      <div className="panel-heading">
        <div>
          <h2>De dónde viene</h2>
          <p>Distribución de tokens</p>
        </div>
        <select
          aria-label="Distribución por"
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          <option value="models">Modelo</option>
          <option value="agents">Agente</option>
        </select>
      </div>
      {rows.length ? (
        rows.map((r, i) => (
          <details className="dist-row" key={r.name}>
            <summary>
              <span>
                <i className={i % 2 ? 'violet' : 'mint'} />
                {r.name}
              </span>
              <strong>{compact(r.totalTokens)}</strong>
              <svg viewBox="0 0 400 5" preserveAspectRatio="none" aria-hidden="true">
                <rect width="400" height="5" rx="2" fill="#26332e" />
                <rect
                  width={(Number(r.totalTokens) / max) * 400}
                  height="5"
                  rx="2"
                  fill={i % 2 ? '#b59add' : '#a5edc6'}
                />
              </svg>
            </summary>
            <p>
              Entrada: {exact(r.inputTokens)} · Salida: {exact(r.outputTokens)}
              <br />
              {r.count} respuestas · USD {r.costUsd} · {r.unpriced} sin estimar
            </p>
          </details>
        ))
      ) : (
        <div className="empty">Todavía no hay respuestas en este periodo.</div>
      )}
      <div className="quiet-note">Cada respuesta cuenta una sola vez.</div>
    </section>
  );
}
const sourceLabels = {
  codex: 'Codex',
  opencode: 'OpenCode',
  gemini: 'Gemini CLI',
  antigravity: 'Antigravity',
};
const stateLabels = {
  running: 'Activo · cada 2 s',
  waiting: 'Esperando registros',
  unsupported: 'Formato no verificable',
  error: 'Revisar lectura',
  incomplete: 'Esperando escritura completa',
  starting: 'Iniciando',
};
function Breakdown({ data }) {
  const t = data.totals;
  return (
    <section className="panel breakdown">
      <div className="panel-heading">
        <div>
          <h2>Qué se está contando</h2>
          <p>Consumo por llamada · los desgloses ya están incluidos en los totales</p>
        </div>
      </div>
      <div className="breakdown-grid">
        <div>
          <span>Entrada normal</span>
          <strong>{exact(t.normalInputTokens)}</strong>
          <small>
            USD {t.costInputUsd}
            {t.unpriced || t.missingCache ? ' · subtotal conocido' : ''}
          </small>
        </div>
        <div>
          <span>Caché leída / escrita</span>
          <strong>
            {exact(t.cachedInputTokens)} / {exact(t.cacheWriteInputTokens)}
          </strong>
          <small>
            USD {t.costCacheReadUsd} / {t.costCacheWriteUsd}
            {t.unpriced || t.missingCache ? ' · subtotal conocido' : ''}
          </small>
        </div>
        <div>
          <span>Salida visible / razonamiento</span>
          <strong>
            {exact(t.visibleOutputTokens)} / {exact(t.reasoningOutputTokens)}
          </strong>
          <small>
            USD {t.costVisibleOutputUsd} / {t.costReasoningUsd}
            {t.unpriced ? ' · subtotal conocido' : ''}
            {t.missingReasoning ? ' · desglose parcial' : ''}
          </small>
        </div>
      </div>
      <p className="explanation">
        Cada respuesta puede volver a leer el historial, las instrucciones y las herramientas. Por
        eso los tokens procesados pueden superar ampliamente el texto escrito. La caché se factura a
        una tarifa distinta en esta equivalencia.
      </p>
    </section>
  );
}
function SourcesPanel({ sources, reconciliation, agent }) {
  return (
    <section className="panel sources">
      <div className="panel-heading">
        <div>
          <h2>Fuentes y precisión</h2>
          <p>Lectura pasiva · no modifica los agentes ni sus registros</p>
        </div>
      </div>
      <div className="source-grid">
        {Object.entries(sourceLabels)
          .filter(([, name]) => !agent || name === agent)
          .map(([key, name]) => (
            <div key={key}>
              <strong>{name}</strong>
              <span>{stateLabels[sources?.[key]?.state] ?? 'No configurado'}</span>
              <small>{sources?.[key]?.reason ?? sources?.[key]?.version ?? ''}</small>
            </div>
          ))}
      </div>
      <div className="source-detail">
        <p>
          Tokenizadores locales:{' '}
          {sources?.tokenizer?.state === 'ready' ? 'disponibles, sin red' : 'no disponibles'}.
          tiktoken {sources?.tokenizer?.versions?.tiktoken ?? '—'} · Google SDK{' '}
          {sources?.tokenizer?.versions?.['google-genai'] ?? '—'}.
        </p>
        <p>
          GPT-6/Astra: se utiliza el contador nativo; tiktoken no publica una correspondencia para
          estos modelos. Google LocalTokenizer es experimental y solo cuenta texto. Las estimaciones
          locales parciales se mantienen fuera de los totales y del coste.
        </p>
        <details>
          <summary>Modelos de Google con tokenizador local instalado</summary>
          <p>{sources?.tokenizer?.googleModels?.join(' · ') ?? 'Sin información'}</p>
          <p>
            Vocabulario no instalado: {sources?.tokenizer?.googleUnavailable?.join(' · ') ?? '—'}.
            Otros modelos requieren un mapeo oficial antes de activar la estimación.
          </p>
        </details>
        {reconciliation && (
          <>
            <h3>Conciliación de esta selección</h3>
            <p>
              <strong>{reconciliation.matched}</strong> respuestas coinciden con su registro nativo
              · {reconciliation.conflicts} conflictos · {reconciliation.unverified} sin observación
              nativa.
            </p>
            <p>{reconciliation.note}</p>
            <details>
              <summary>{reconciliation.sessions.length} sesiones de origen</summary>
              {reconciliation.sessions.map((s) => (
                <p className="response-id" key={s.sessionId}>
                  {s.sessionId} · {s.count} respuestas · Entrada {exact(s.inputTokens)} · Caché{' '}
                  {exact(s.cachedInputTokens)} · Salida {exact(s.outputTokens)}
                </p>
              ))}
            </details>
            <details>
              <summary>
                Estimaciones de texto visible ({reconciliation.textMeasurements.length})
              </summary>
              {reconciliation.textMeasurements.length ? (
                reconciliation.textMeasurements.map((m, i) => (
                  <p key={i}>
                    {m.agent} · {m.model} · {m.status} · {exact(m.tokens)} tokens de texto visible ·{' '}
                    {m.tokenizer} {m.version}. Entrada, herramientas y razonamiento no
                    reconstruidos.
                  </p>
                ))
              ) : (
                <p>No hay respuestas sin contador nativo con texto compatible para estimar.</p>
              )}
            </details>
          </>
        )}
      </div>
    </section>
  );
}
function App() {
  const [session, setSession] = useState(null),
    [data, setData] = useState(null),
    [history, setHistory] = useState(null),
    [window, setWindow] = useState('1h'),
    [agent, setAgent] = useState(''),
    [model, setModel] = useState(''),
    [provider, setProvider] = useState(''),
    [sessionId, setSessionId] = useState(''),
    [method, setMethod] = useState(''),
    [sources, setSources] = useState(null),
    [reconciliation, setReconciliation] = useState(null),
    [offset, setOffset] = useState(0),
    [connection, setConnection] = useState('Conectando'),
    [error, setError] = useState(''),
    [clock, setClock] = useState(Date.now()),
    [tab, setTab] = useState('overview'),
    [loadedQuery, setLoadedQuery] = useState('');
  const ended = useRef(false),
    current = useRef({ window, agent, model, provider, sessionId, method, offset }),
    trigger = useRef(() => {});
  current.current = { window, agent, model, provider, sessionId, method, offset };
  const clear = () => {
    if (ended.current) return;
    ended.current = true;
    setData(null);
    setHistory(null);
    setSources(null);
    setReconciliation(null);
    setSession(null);
    location.replace('/access');
  };
  useEffect(() => {
    let stopped = false,
      abort,
      stream,
      timer,
      refreshTimer,
      expiryTimer,
      last = 0,
      running = false,
      dirty = false;
    const api = async (url) => {
      const r = await fetch(url, { cache: 'no-store', signal: abort?.signal });
      if (r.status === 401) {
        clear();
        throw Error('Acceso caducado');
      }
      if (!r.ok) throw Error('No se pudo actualizar el panel');
      return r.json();
    };
    const refresh = async () => {
      if (stopped) return;
      if (running) {
        dirty = true;
        return;
      }
      const delay = 1000 - (Date.now() - last);
      if (delay > 0) {
        clearTimeout(timer);
        timer = setTimeout(refresh, delay);
        return;
      }
      last = Date.now();
      running = true;
      dirty = false;
      abort = new AbortController();
      const f = { ...current.current };
      try {
        const q = new URLSearchParams({
          window: f.window,
          agent: f.agent,
          model: f.model,
          provider: f.provider,
          sessionId: f.sessionId,
          method: f.method,
          to: String(Date.now()),
        });
        const [a, h, s, r] = await Promise.all([
          api('/api/analytics?' + q),
          api('/api/events?' + q + '&offset=' + f.offset),
          api('/api/sources'),
          api('/api/reconciliation?' + q),
        ]);
        if (!stopped && !ended.current && JSON.stringify(f) === JSON.stringify(current.current)) {
          setData(a);
          setHistory(h);
          setSources(s);
          setReconciliation(r);
          setLoadedQuery(JSON.stringify(f));
          setError('');
        }
      } catch (e) {
        if (!stopped && e.name !== 'AbortError')
          setError('No se pudo actualizar. Se muestran los últimos datos recibidos.');
      } finally {
        running = false;
        if (dirty) refresh();
      }
    };
    trigger.current = refresh;
    api('/auth/session')
      .then((s) => {
        if (stopped) return;
        setSession(s);
        expiryTimer = setTimeout(clear, Math.max(0, Date.parse(s.expiresAt) - Date.now()));
        refresh();
        stream = new EventSource('/api/stream');
        stream.addEventListener('ready', () => {
          setConnection('En vivo');
          refresh();
        });
        stream.addEventListener('change', refresh);
        stream.addEventListener('expired', clear);
        stream.onerror = () => {
          setConnection('Reconectando');
          api('/auth/session').catch(() => {});
        };
        refreshTimer = setInterval(refresh, 10000);
      })
      .catch(() => {
        if (!stopped) setError('No se pudo conectar. Recarga la página para reintentar.');
      });
    const tick = setInterval(() => setClock(Date.now()), 1000);
    const visible = () => {
      if (document.visibilityState === 'visible') {
        api('/auth/session').catch(() => {});
        refresh();
      }
    };
    document.addEventListener('visibilitychange', visible);
    return () => {
      stopped = true;
      abort?.abort();
      stream?.close();
      clearTimeout(timer);
      clearTimeout(expiryTimer);
      clearInterval(refreshTimer);
      clearInterval(tick);
      document.removeEventListener('visibilitychange', visible);
    };
  }, []);
  useEffect(() => {
    trigger.current();
  }, [window, agent, model, provider, sessionId, method, offset]);
  const change = (setter, v) => {
    setOffset(0);
    setter(v);
  };
  const logout = async () => {
    try {
      const r = await fetch('/auth/logout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf },
        body: '{}',
      });
      if (!r.ok && r.status !== 401) throw Error();
      clear();
    } catch {
      setError('No se pudo cerrar la sesión. Reintenta cuando haya conexión.');
    }
  };
  const selectAgent = (name) => {
    setAgent(name);
    setModel('');
    setProvider('');
    setSessionId('');
    setMethod('');
    setOffset(0);
    setTab('overview');
  };
  const agents = agentList(data?.options.agents ?? []),
    selectedAgent = agents.find((x) => x.name === agent),
    pending = loadedQuery !== JSON.stringify(current.current);
  const remaining = session ? Math.max(0, Date.parse(session.expiresAt) - clock) : 0;
  const remainingText = `${Math.floor(remaining / 3600000)} h ${Math.floor((remaining % 3600000) / 60000)} min`;
  return (
    <div className="shell">
      <aside>
        <a className="brand" href="/" aria-label="Monitor de consumo, inicio">
          <Icon />
          Consumo IA<span>●</span>
        </a>
        <span className="nav-label">WORKSPACE</span>
        <nav aria-label="Principal">
          <button
            className={tab === 'overview' && !agent ? 'active' : ''}
            onClick={() => selectAgent('')}
          >
            <Icon type="grid" />
            Vista general
          </button>
          <button className={tab === 'history' ? 'active' : ''} onClick={() => setTab('history')}>
            <span className="history-icon">↺</span>Actividad
          </button>
        </nav>
        <span className="nav-label agents-label">AGENTES</span>
        <AgentNavigation agents={agents} selected={agent} onSelect={selectAgent} />
        <div className="sidebar-bottom">
          <span className="lock">◇</span>
          <div>
            Espacio privado<small>Solo este navegador</small>
          </div>
        </div>
      </aside>
      <div className="main-wrap">
        <header>
          <span>
            Workspace <b>/</b>{' '}
            <strong>{agent || (tab === 'overview' ? 'Vista general' : 'Actividad')}</strong>
          </span>
          <div className="header-right">
            <span
              className="session"
              title={session ? `Caduca ${fulltime(session.expiresAt)}` : ''}
            >
              ◷ {remainingText}
            </span>
            <button
              className="icon-button"
              onClick={logout}
              disabled={!session}
              aria-label="Cerrar sesión"
              title="Cerrar sesión"
            >
              <Icon type="power" />
            </button>
          </div>
        </header>
        <main>
          <div className="page-title">
            <div>
              <div className="eyebrow">OBSERVABILIDAD DE IA</div>
              <h1>
                {agent
                  ? `${agent}, en tiempo real.`
                  : tab === 'overview'
                    ? 'Cada token, en perspectiva.'
                    : 'El detalle de cada respuesta.'}
              </h1>
              <p>
                {agent
                  ? 'Métricas, gráficas y actividad de este agente.'
                  : 'Una vista clara del consumo de tus agentes.'}
              </p>
            </div>
            <span className={'live ' + (connection === 'En vivo' ? '' : 'offline')}>
              <i />
              {connection}
            </span>
          </div>
          <div className="toolbar">
            <div className="segments" aria-label="Ventana de tiempo">
              {[
                ['15m', '15 min'],
                ['1h', '1 hora'],
                ['24h', '24 horas'],
                ['7d', '7 días'],
              ].map(([v, l]) => (
                <button
                  key={v}
                  aria-pressed={window === v}
                  className={window === v ? 'selected' : ''}
                  onClick={() => change(setWindow, v)}
                >
                  {l}
                </button>
              ))}
            </div>
            <div className="filters">
              <label>
                Agente{' '}
                <select
                  aria-label="Filtrar por agente"
                  value={agent}
                  onChange={(e) => selectAgent(e.target.value)}
                >
                  <option value="">Todos los agentes</option>
                  {agents.map((x) => (
                    <option key={x.name}>{x.name}</option>
                  ))}
                </select>
              </label>
              <label>
                Modelo{' '}
                <select
                  aria-label="Filtrar por modelo"
                  value={model}
                  onChange={(e) => change(setModel, e.target.value)}
                >
                  <option value="">Todos los modelos</option>
                  {data?.options.models.map((x) => (
                    <option key={x}>{x}</option>
                  ))}
                </select>
              </label>
              <label>
                Proveedor
                <select
                  aria-label="Filtrar por proveedor"
                  value={provider}
                  onChange={(e) => change(setProvider, e.target.value)}
                >
                  <option value="">Todos los proveedores</option>
                  {data?.options.providers?.map((x) => (
                    <option key={x}>{x}</option>
                  ))}
                </select>
              </label>
              <label>
                Sesión
                <select
                  aria-label="Filtrar por sesión"
                  value={sessionId}
                  onChange={(e) => change(setSessionId, e.target.value)}
                >
                  <option value="">Todas las sesiones</option>
                  {data?.options.sessions?.map((x) => (
                    <option key={x} value={x}>
                      {x}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Método
                <select
                  aria-label="Filtrar por método"
                  value={method}
                  onChange={(e) => change(setMethod, e.target.value)}
                >
                  <option value="">Todos los métodos</option>
                  {data?.options.methods?.map((x) => (
                    <option key={x} value={x}>
                      {x === 'native'
                        ? 'Contador nativo'
                        : x === 'local_text'
                          ? 'Texto local (parcial)'
                          : 'Métrica declarada'}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </div>
          {error && (
            <div className="notice" role="alert">
              {error}
            </div>
          )}
          {!data || pending ? (
            <div className="empty loading" role="status">
              {agent ? `Cargando métricas de ${agent}…` : 'Preparando tu espacio…'}
            </div>
          ) : (
            <>
              {selectedAgent ? (
                <AgentStatus
                  agent={selectedAgent}
                  data={data}
                  source={sources?.[selectedAgent.source]}
                  connected={connection === 'En vivo'}
                />
              ) : (
                tab === 'overview' && (
                  <AgentSections
                    agents={agents}
                    data={data}
                    sources={sources}
                    connected={connection === 'En vivo'}
                    onSelect={selectAgent}
                  />
                )
              )}
              <div className="stats">
                <article className="stat">
                  <div>
                    Tokens totales <span>◈</span>
                  </div>
                  <strong title={exact(data.totals.totalTokens)}>
                    {compact(data.totals.totalTokens)}
                  </strong>
                  <small>{exact(data.totals.totalTokens)} tokens exactos</small>
                </article>
                <article className="stat">
                  <div>
                    Entrada <span className="mint-text">↓</span>
                  </div>
                  <strong>{compact(data.totals.inputTokens)}</strong>
                  <small>
                    {exact(data.totals.cachedInputTokens)} en caché{' '}
                    {data.totals.missingCache > 0 ? '· parcial' : ''}
                  </small>
                </article>
                <article className="stat">
                  <div>
                    Salida <span className="violet-text">↑</span>
                  </div>
                  <strong>{compact(data.totals.outputTokens)}</strong>
                  <small>
                    {exact(data.totals.reasoningOutputTokens)} de razonamiento{' '}
                    {data.totals.missingReasoning > 0 ? '· parcial' : ''}
                  </small>
                </article>
                <article className="stat cost">
                  <div>
                    Equivalente API estimado <span>↗</span>
                  </div>
                  <strong>
                    {data.totals.count === data.totals.unpriced && data.totals.count > 0
                      ? 'Sin estimar'
                      : money(data.totals.costUsd)}
                  </strong>
                  <small>
                    {data.totals.unpriced
                      ? `${data.totals.unpriced} respuestas sin estimar`
                      : `USD · ${data.totals.count} respuestas`}
                  </small>
                </article>
              </div>
              {(data.totals.missingCache > 0 ||
                data.totals.missingReasoning > 0 ||
                data.errorCount > 0) && (
                <div className="notice">
                  {data.totals.missingCache > 0 &&
                    `${data.totals.missingCache} respuestas con caché incompleta. `}
                  {data.totals.missingReasoning > 0 &&
                    `${data.totals.missingReasoning} sin desglose de razonamiento. `}
                  {data.errorCount > 0 && (
                    <details>
                      <summary>{data.errorCount} incidencias del recolector</summary>
                      {data.errors.map((e, i) => (
                        <p key={i}>
                          {fulltime(e.at)} · {e.kind}
                        </p>
                      ))}
                    </details>
                  )}
                </div>
              )}
              <div className="subscription-note">
                <strong>Equivalencia de API, no gasto de tu suscripción.</strong> AI Usage Monitor
                no consulta tu factura ni atribuye cargos adicionales. Los importes simulan este
                consumo con tarifas públicas.
              </div>
              {tab === 'overview' && (
                <>
                  <Breakdown data={data} />
                  <Chart key={`tokens-${agent}`} data={data} />
                  <div className="lower-grid">
                    <Chart key={`cost-${agent}`} data={data} cost />
                    <Distribution data={data} />
                  </div>
                </>
              )}
              <section className="panel history">
                <div className="panel-heading">
                  <div>
                    <h2>
                      {agent ? `Actividad de ${agent}` : 'Actividad reciente'}{' '}
                      <span className="count">{history?.total ?? 0}</span>
                    </h2>
                    <p>Respuestas registradas · sin prompts ni contenido</p>
                  </div>
                  <span className="subtle">
                    {history
                      ? `${Math.min(offset + 1, history.total)}–${Math.min(offset + 50, history.total)}`
                      : ''}
                  </span>
                </div>
                <div className="table-scroll">
                  <table>
                    <thead>
                      <tr>
                        <th>Momento</th>
                        <th>Agente / modelo</th>
                        <th>Entrada</th>
                        <th>Salida</th>
                        <th>Equivalente API</th>
                        <th>Detalle</th>
                      </tr>
                    </thead>
                    <tbody>
                      {history?.events.map((e) => (
                        <tr key={e.id}>
                          <td>
                            <time dateTime={e.occurredAt} title={fulltime(e.occurredAt)}>
                              {fulltime(e.occurredAt)}
                            </time>
                          </td>
                          <td>
                            <span className="agent-badge">{e.agent}</span>
                            <small>{e.model}</small>
                          </td>
                          <td>{exact(e.inputTokens)}</td>
                          <td>{exact(e.outputTokens)}</td>
                          <td title={e.costUsd ? `USD ${e.costUsd}` : e.costNote}>
                            {money(e.costUsd)}
                          </td>
                          <td>
                            <details>
                              <summary aria-label={`Detalles de respuesta ${e.id}`}>Ver</summary>
                              <div className="event-detail">
                                Caché leída: {exact(e.cachedInputTokens)}
                                <br />
                                Caché escrita: {exact(e.cacheWriteInputTokens)}
                                <br />
                                Razonamiento: {exact(e.reasoningOutputTokens)}
                                <br />
                                USD entrada: {e.costInputUsd ?? '—'}
                                <br />
                                USD caché leída / escrita: {e.costCacheReadUsd ?? '—'} /{' '}
                                {e.costCacheWriteUsd ?? '—'}
                                <br />
                                USD salida visible / razonamiento: {e.costVisibleOutputUsd ??
                                  '—'} / {e.costReasoningUsd ?? '—'}
                                <br />
                                {e.costNote}
                                <br />
                                {e.source} · {e.provider}
                                <br />
                                Método: {e.method === 'native' ? 'contador nativo' : 'declarado'}
                                <br />
                                Sesión: {e.sessionId ?? 'desconocida'}
                                <br />
                                Turno: {e.turnId ?? 'desconocido'}
                                <br />
                                Formato: {e.sourceVersion ?? 'sin verificar'}
                                <br />
                                Cobertura:{' '}
                                {e.coverage === 'agent_normalized_usage_zero_defaults_possible'
                                  ? 'Contador del agente; algunos ceros pueden ser valores por defecto'
                                  : e.coverage === 'provider_response_usage'
                                    ? 'Uso nativo por respuesta'
                                    : (e.coverage ?? 'desconocida')}
                                <br />
                                Tarifas: {e.pricingVersion ?? 'registro anterior'} · Standard
                                supuesto
                                <br />
                                <span className="response-id">{e.responseId}</span>
                              </div>
                            </details>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                {!history?.total && (
                  <div className="empty">
                    <Icon />
                    <h3>Esperando la próxima respuesta</h3>
                    <p>
                      {agent
                        ? `${agent} aparecerá aquí al recibir métricas verificables.`
                        : 'AI Usage Monitor espera consumo nuevo de las fuentes compatibles.'}
                      <br />
                      Prueba otra ventana o deja que tu agente siga trabajando.
                    </p>
                  </div>
                )}
                {!!history?.total && (
                  <div className="pagination">
                    <span>{history.total} respuestas en esta selección</span>
                    <div>
                      <button
                        disabled={offset === 0}
                        onClick={() => setOffset(Math.max(0, offset - 50))}
                      >
                        ← Anterior
                      </button>
                      <button
                        disabled={offset + 50 >= history.total}
                        onClick={() => setOffset(offset + 50)}
                      >
                        Siguiente →
                      </button>
                    </div>
                  </div>
                )}
              </section>
              <SourcesPanel sources={sources} reconciliation={reconciliation} agent={agent} />
              <details className="pricing-note">
                <summary>Cómo se estima el coste</summary>
                <p>
                  {data.pricingNote} Los desgloses ausentes se muestran como desconocidos; los
                  subtotales solo incluyen valores disponibles.{' '}
                  <a
                    href="https://developers.openai.com/api/docs/pricing"
                    target="_blank"
                    rel="noreferrer"
                  >
                    OpenAI ↗
                  </a>{' '}
                  ·{' '}
                  <a
                    href="https://ai.google.dev/gemini-api/docs/pricing"
                    target="_blank"
                    rel="noreferrer"
                  >
                    Google ↗
                  </a>
                </p>
              </details>
              <footer>
                <div>
                  <i className={connection === 'En vivo' ? 'mint' : 'violet'} />
                  {agent || 'Todos los agentes'} ·{' '}
                  {connection === 'En vivo' ? 'Actualización en vivo' : 'Reconectando'}
                  <span>Los lectores activos revisan nuevos registros cada 2 s</span>
                </div>
                <span>
                  Actualizado {time(data.updatedAt)} · Recogida desde {fulltime(data.activation)}
                </span>
              </footer>
            </>
          )}
        </main>
      </div>
    </div>
  );
}
createRoot(document.getElementById('root')).render(<App />);
