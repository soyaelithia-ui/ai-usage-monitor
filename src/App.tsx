import { useEffect, useRef, useState } from 'react';
import {
  Activity, ArrowDownLeft, ArrowUpRight, Bot, Check, ChevronRight, CircleHelp,
  Code2, Copy, Database, ExternalLink, Layers3, LayoutDashboard, ListFilter,
  Radio, RefreshCw, Server, Terminal, Wallet, X, Zap,
} from 'lucide-react';
import type { EventPage, Group, PricingResponse, Snapshot, Totals, UsageEvent } from '../shared/types';
import { useLiveUsage } from './useLiveUsage';

const integer = (value: string | number) => new Intl.NumberFormat('es-ES').format(BigInt(value));
const compact = (value: string | number) => new Intl.NumberFormat('es-ES', { notation: 'compact', maximumFractionDigits: 1 }).format(Number(value));
const amount = (value: string) => {
  const number = Number(value);
  if (number > 0 && number < 0.000001) return '< 0,000001';
  return new Intl.NumberFormat('es-ES', { minimumFractionDigits: 2, maximumFractionDigits: 6 }).format(number);
};
const time = (value: string) => new Date(value).toLocaleTimeString('es-ES', { hour12: false });
const emptyTotals: Totals = { requests: 0, prompt_tokens: '0', completion_tokens: '0', total_tokens: '0', cost_usd: '0', unpriced_requests: 0 };
const palette = ['#7ae8cd', '#a79aff', '#edc37d', '#7bbcee', '#ed9bbb', '#a7ce82'];
const colorFor = (name: string) => palette[[...name].reduce((hash, char) => (hash * 31 + char.charCodeAt(0)) >>> 0, 0) % palette.length];

function Cost({ value }: { value: string | null }) {
  return value === null ? <span className="unpriced">Sin tarifa</span>
    : <span className="money" title={`${value} USD`}>${amount(value)}</span>;
}

function Breakdown({ groups, type, ready }: { groups: Group[]; type: 'agent' | 'model'; ready: boolean }) {
  const isAgent = type === 'agent';
  const maxTokens = Math.max(1, ...groups.map(group => Number(group.total_tokens)));
  return <section className="panel breakdown">
    <div className="panel-heading">
      <div className="heading-with-icon">{isAgent ? <Bot size={18} /> : <Layers3 size={18} />}<h2>Por {isAgent ? 'agente' : 'modelo'}</h2><span className="count-badge">{groups.length}</span></div>
      <span className="tiny-label">TOKENS / GASTO</span>
    </div>
    <div className="breakdown-content">
      {!groups.length ? <div className="empty-group">
        <span className="empty-symbol">{isAgent ? <Bot size={25} /> : <Layers3 size={25} />}</span>
        <strong>{ready ? isAgent ? 'Esperando al primer agente' : 'Tus modelos aparecerán aquí' : 'Conectando con el monitor'}</strong>
        <p>{ready ? 'El desglose se actualiza con cada petición.' : 'Cargando métricas guardadas…'}</p>
        <div className="placeholder-bars"><i /><i /><i /></div>
      </div> : groups.map(group => {
        const inputWidth = Number(group.prompt_tokens) / maxTokens * 100;
        const outputWidth = Number(group.completion_tokens) / maxTokens * 100;
        return <div className="group-row" key={group.name}>
          <div className="group-topline"><span className="group-name" title={group.name}><i style={{ background: colorFor(group.name) }} />{group.name}</span>
            <Cost value={group.unpriced_requests === group.requests ? null : group.cost_usd} /></div>
          <div className="token-track" title={`Entrada: ${integer(group.prompt_tokens)} · Salida: ${integer(group.completion_tokens)}`}>
            <span className="input-bar" style={{ width: `${inputWidth}%` }} /><span className="output-bar" style={{ width: `${outputWidth}%` }} />
          </div>
          <div className="group-meta"><span>{integer(group.prompt_tokens)} <span className="muted">in</span><span className="meta-divider">/</span>{integer(group.completion_tokens)} <span className="muted">out</span></span>
            <span>{integer(group.requests)} {group.requests === 1 ? 'petición' : 'peticiones'}{group.unpriced_requests > 0 && <span className="unpriced-count" title="Eventos excluidos del gasto"> · {group.unpriced_requests} sin tarifa</span>}</span></div>
        </div>;
      })}
    </div>
    <div className="panel-footer"><span className="legend"><i className="input-dot" />Entrada<i className="output-dot" />Salida</span><span>Acumulado histórico</span></div>
  </section>;
}

function History({ data, ready, connect }: { data: Snapshot | null; ready: boolean; connect: () => void }) {
  const [archive, setArchive] = useState<EventPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const requestVersion = useRef(0);
  useEffect(() => () => { requestVersion.current++; }, []);
  const events = archive?.events ?? data?.events ?? [];
  const cursor = archive ? archive.next_before_id : data?.next_before_id;
  async function older() {
    const version = ++requestVersion.current;
    setLoading(true); setError('');
    try {
      const response = await fetch(`/api/events?before_id=${cursor}&limit=100`);
      if (!response.ok) throw new Error('No se pudo cargar el historial. Vuelve a intentarlo.');
      const page = await response.json() as EventPage;
      if (version === requestVersion.current) setArchive(page);
    } catch (error) { if (version === requestVersion.current) setError((error as Error).message); }
    finally { if (version === requestVersion.current) setLoading(false); }
  }
  return <section className="panel history" aria-label="Historial de eventos">
    <div className="panel-heading"><div className="heading-with-icon"><ListFilter size={18} /><h2>Actividad reciente</h2></div>
      {archive ? <button className="text-button" onClick={() => { requestVersion.current++; setArchive(null); setLoading(false); setError(''); }}>Volver en vivo <Radio size={13} /></button>
        : <span className="live-caption"><i />ACTUALIZACIÓN EN VIVO</span>}
    </div>
    {events.length ? <div className="table-scroll"><table>
      <thead><tr><th>HORA</th><th>AGENTE</th><th>MODELO</th><th className="numeric">ENTRADA</th><th className="numeric">SALIDA</th><th className="numeric">COSTO USD</th></tr></thead>
      <tbody>{events.map(event => <tr key={event.id} data-event-id={event.id}>
        <td className="timestamp" title={new Date(event.received_at).toLocaleString('es-ES')}><span>{time(event.received_at)}</span><small>{new Date(event.received_at).toLocaleDateString('es-ES')}</small></td>
        <td><span className="event-agent" title={event.agent_name}><span className="agent-dot" style={{ background: colorFor(event.agent_name) }} />{event.agent_name}</span></td>
        <td><span className="model-pill" title={event.model}>{event.model}</span></td>
        <td className="numeric muted">{integer(event.prompt_tokens)}</td><td className="numeric muted">{integer(event.completion_tokens)}</td>
        <td className="numeric"><Cost value={event.cost_usd} /></td>
      </tr>)}</tbody>
    </table></div> : <div className="empty-history"><div className="empty-orbit"><Radio size={26} /></div>
      <h3>{ready ? 'A la escucha de tus agentes' : 'Preparando el historial'}</h3>
      <p>{ready ? 'Envía tu primera métrica y sigue cada petición desde aquí.' : 'El historial aparecerá cuando se conecte el backend.'}</p>
      <button className="secondary-button" onClick={connect}><Code2 size={15} />Ver cómo conectar un agente<ChevronRight size={15} /></button>
    </div>}
    {error && <p role="alert" className="inline-error">{error}</p>}
    <div className="panel-footer"><span>{events.length ? `${archive ? 'Historial' : 'Últimos eventos'} · ${events.length} de ${integer(data?.totals.requests ?? 0)} peticiones` : 'Tus eventos se conservan entre reinicios'}</span>
      <button className="text-button" disabled={!cursor || loading} onClick={older}>{loading ? 'Cargando…' : 'Ver anteriores'}<ChevronRight size={14} /></button></div>
  </section>;
}

function Pricing() {
  const [catalog, setCatalog] = useState<PricingResponse | null>(null);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError('');
    fetch('/api/pricing', { signal: controller.signal }).then(response => {
      if (!response.ok) throw new Error('No se pudo cargar el catálogo.');
      return response.json();
    }).then(setCatalog).catch(error => { if (!controller.signal.aborted) setError(error.message); });
    return () => controller.abort();
  }, [attempt]);
  return <section className="panel pricing-panel">
    <div className="panel-heading"><div className="heading-with-icon"><Wallet size={18} /><h2>Catálogo de precios</h2></div><span className="tiny-label">USD / MILLÓN DE TOKENS</span></div>
    <div className="pricing-explanation"><Code2 size={20} /><div><strong>Tu configuración, tus tarifas.</strong><p>Edita <code>config/pricing.json</code> y reinicia el backend. Cada evento conserva el precio con el que fue registrado.</p></div></div>
    {error && <div className="inline-error" role="alert">{error} <button className="text-button" onClick={() => setAttempt(value => value + 1)}>Reintentar</button></div>}
    {!catalog && !error && <p className="loading-message">Cargando tarifas…</p>}
    {catalog && <><div className="table-scroll"><table><thead><tr><th>MODELO</th><th>PROVEEDOR</th><th className="numeric">ENTRADA</th><th className="numeric">SALIDA</th><th>CONDICIONES</th><th>FUENTE</th></tr></thead><tbody>
      {Object.entries(catalog.models).map(([name, model]) => {
        const rate = model.rates.findLast(rate => rate.effective_from <= catalog.server_date);
        const future = model.rates.find(rate => rate.effective_from > catalog.server_date);
        return <tr key={name}><td><span className="model-pill">{name}</span></td><td className="muted">{model.provider}</td><td className="numeric"><Cost value={rate?.input_per_million ?? null} /></td><td className="numeric"><Cost value={rate?.output_per_million ?? null} /></td>
          <td className="rate-notes">{rate?.long_context && <span>Más de {integer(rate.long_context.above_prompt_tokens)} tokens:<br /><Cost value={rate.long_context.input_per_million} /> / <Cost value={rate.long_context.output_per_million} /></span>}
            {future && <span>Desde {future.effective_from}:<br /><Cost value={future.input_per_million} /> / <Cost value={future.output_per_million} /></span>}
            {!rate?.long_context && !future && <span>Tarifa estándar</span>}</td>
          <td><a className="source-link" href={model.source} target="_blank" rel="noreferrer" aria-label={`Fuente oficial de ${name}`}><ExternalLink size={15} /></a></td></tr>;
      })}
    </tbody></table></div><div className="panel-footer"><span>Verificación: {catalog.verified_at}</span><span>{Object.keys(catalog.models).length} modelos configurados</span></div></>}
    <p className="pricing-note">Estimación de texto a tarifa estándar de pago. Excluye caché, Batch, herramientas, impuestos y descuentos particulares. Las tarifas se mantienen manualmente.</p>
  </section>;
}

function ConnectDialog({ close }: { close: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const isLocal = ['localhost', '127.0.0.1', '[::1]'].includes(window.location.hostname);
  const endpoint = isLocal ? 'http://127.0.0.1:3000' : window.location.origin;
  const authorization = isLocal ? '' : `  -H 'Authorization: Bearer <TOKEN_DE_INGESTA>' \\\n`;
  const curl = `curl ${endpoint}/api/usage \\\n  -H 'Content-Type: application/json' \\\n${authorization}  -d '{\n    "agent_name": "investigador",\n    "model": "gpt-6.1-sol",\n    "prompt_tokens": 1000,\n    "completion_tokens": 500,\n    "request_id": "peticion-001"\n  }'`;
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="connect-dialog" onCancel={close} onClick={event => { if (event.target === dialog.current) close(); }}>
    <div className="dialog-top"><span className="dialog-icon"><Terminal size={22} /></span><button className="icon-button" onClick={close} aria-label="Cerrar instrucciones"><X size={20} /></button></div>
    <span className="eyebrow">EMPIEZA EN UNA PETICIÓN</span><h2>Conecta tu primer agente</h2>
    <p>Envía el uso al terminar cada llamada a tu modelo. Pulse calcula el gasto y actualiza todas las pestañas abiertas.</p>
    <div className="code-heading"><span><i className="input-dot" />POST /api/usage</span><button className="text-button" onClick={async () => { try { await navigator.clipboard.writeText(curl); setCopied(true); setCopyError(false); } catch { setCopyError(true); } }}>{copied ? <Check size={14} /> : <Copy size={14} />}{copied ? 'Copiado' : 'Copiar cURL'}</button></div>
    <pre tabIndex={0}>{curl}</pre>
    {copyError && <p role="alert">No se pudo copiar. Selecciona el ejemplo para copiarlo manualmente.</p>}
    <div className="dialog-tip"><CircleHelp size={16} /><p>Usa un <code>request_id</code> distinto por llamada. Reutiliza el mismo al reintentar su envío para evitar contar tokens dos veces.{!isLocal && <> Para enviar métricas desde Internet, reemplaza <code>&lt;TOKEN_DE_INGESTA&gt;</code> por el contenido de <code>data/ingest-token</code> en el servidor.</>}</p></div>
    <div className="dialog-footer"><span><Database size={13} />Guardado local en SQLite</span><button className="primary-button" onClick={close}>Entendido<ChevronRight size={15} /></button></div>
  </dialog>;
}

function MiniActivity({ events }: { events: UsageEvent[] }) {
  const values = events.slice(0, 28).reverse().map(event => Number(event.cost_usd ?? '0'));
  const max = Math.max(...values, 0.000001);
  return <div className="mini-activity" aria-hidden="true">{Array.from({ length: 28 }, (_, index) => {
    const value = values[index - (28 - values.length)];
    return <i key={index} className={value === undefined ? 'inactive' : ''} style={{ height: value === undefined ? '4px' : `${Math.max(6, value / max * 45)}px` }} />;
  })}</div>;
}

export function App() {
  const { data, connection, updatedAt } = useLiveUsage();
  const [view, setView] = useState<'overview' | 'activity' | 'pricing'>('overview');
  const [connecting, setConnecting] = useState(false);
  const totals = data?.totals ?? emptyTotals;
  const views = [
    { id: 'overview' as const, name: 'Vista general', icon: LayoutDashboard },
    { id: 'activity' as const, name: 'Actividad', icon: Activity },
    { id: 'pricing' as const, name: 'Tarifas', icon: Wallet },
  ];
  return <div className="app-shell">
    <aside className="sidebar"><a className="brand" href="#" onClick={event => { event.preventDefault(); setView('overview'); }}><span className="brand-mark"><Activity size={26} strokeWidth={2.2} /></span><span>pulse<span className="brand-period">.</span></span></a>
      <div className="workspace"><span className="workspace-icon"><Terminal size={17} /></span><div><strong>Mi laboratorio</strong><span>Espacio local</span></div><span className="workspace-led" /></div>
      <span className="nav-label">MONITOREO</span>
      <nav aria-label="Navegación principal">{views.map(({ id, name, icon: Icon }) => <button key={id} className={`nav-item ${view === id ? 'selected' : ''}`} aria-current={view === id ? 'page' : undefined} onClick={() => setView(id)}><Icon size={18} />{name}{id === 'activity' && totals.requests > 0 && <span className="nav-count">{compact(totals.requests)}</span>}</button>)}</nav>
      <div className="sidebar-bottom"><div className="storage-card"><Database size={17} /><strong>Todo queda aquí</strong><p>Métricas e historial guardados en tu máquina.</p><span><i />SQLite persistente</span></div><button className="help-button" onClick={() => setConnecting(true)}><Code2 size={16} />Guía de integración<ArrowUpRight size={14} /></button><div className="sidebar-version"><span>PULSE / EXPERIMENTAL</span><span>v1.0</span></div></div>
    </aside>
    <div className="main-shell">
      <header className="topbar"><div className="breadcrumb"><span>Laboratorio</span><ChevronRight size={13} /><span>{views.find(item => item.id === view)!.name}</span></div><div className="topbar-status"><span className="local-tag"><Server size={12} />{['localhost', '127.0.0.1', '[::1]'].includes(window.location.hostname) ? 'LOCAL' : 'PÚBLICO'}</span><span className={`connection ${connection}`} role="status"><i />{connection === 'online' ? 'En vivo' : connection === 'offline' ? 'Reconectando' : 'Conectando'}</span></div></header>
      <main id="main-content">
        <div className="page-heading"><div><span className="eyebrow">OBSERVABILIDAD DE AGENTES</span><h1>{views.find(item => item.id === view)!.name}</h1><p>{view === 'overview' ? 'El pulso de tus agentes, en tiempo real.' : view === 'activity' ? 'Cada petición tiene una historia. Aquí están todas.' : 'Precios transparentes. Cálculos bajo tu control.'}</p></div><button className="primary-button" aria-label="Conectar agente" onClick={() => setConnecting(true)}><Zap size={16} />Conectar agente<ArrowUpRight size={16} /></button></div>
        {connection === 'offline' && <div className="connection-warning" role="alert"><RefreshCw size={15} /><span>Sin conexión con el backend. Conservamos los últimos datos y volveremos a conectar automáticamente.</span></div>}
        {view !== 'pricing' && <>
          <div className="section-caption"><span><span className="small-square" />RESUMEN ACUMULADO</span><span>Todo el historial<ChevronRight size={12} /></span></div>
          <section className="metrics" aria-label="Resumen de consumo">
            <div className="metric-card spending"><div className="metric-label"><span>Gasto estimado</span><Wallet size={16} /></div><div className="spend-value" data-testid="total-cost" title={`${totals.cost_usd} USD`}><span className="currency-sign">$</span>{data ? amount(totals.cost_usd) : '—'}<span className="currency-label">USD</span></div><div className="spend-bottom"><span><span className="input-dot" />{totals.unpriced_requests ? `${totals.unpriced_requests} ${totals.unpriced_requests === 1 ? 'evento' : 'eventos'} sin tarifa` : 'Calculado por petición'}</span><MiniActivity events={data?.events ?? []} /></div></div>
            <div className="metric-card"><div className="metric-label"><span>Tokens de entrada</span><span className="metric-icon input-icon"><ArrowDownLeft size={17} /></span></div><div className="metric-value" title={integer(totals.prompt_tokens)} data-testid="input-tokens">{data ? compact(totals.prompt_tokens) : '—'}</div><div className="metric-caption"><i className="input-dot" />Contexto y prompts</div></div>
            <div className="metric-card"><div className="metric-label"><span>Tokens de salida</span><span className="metric-icon output-icon"><ArrowUpRight size={17} /></span></div><div className="metric-value" title={integer(totals.completion_tokens)} data-testid="output-tokens">{data ? compact(totals.completion_tokens) : '—'}</div><div className="metric-caption"><i className="output-dot" />Respuestas generadas</div></div>
            <div className="metric-card"><div className="metric-label"><span>Peticiones</span><Activity size={16} /></div><div className="metric-value" data-testid="request-count">{data ? integer(totals.requests) : '—'}</div><div className="metric-caption"><span className="agent-count">{data?.agents.length ?? 0}</span>agentes registrados</div></div>
          </section>
          {totals.unpriced_requests > 0 && <div className="unpriced-notice"><CircleHelp size={16} /><span>El gasto excluye <strong>{totals.unpriced_requests} {totals.unpriced_requests === 1 ? 'evento sin tarifa' : 'eventos sin tarifa'}</strong>. Sus tokens sí están incluidos. Añade los modelos al catálogo para calcular futuras peticiones.</span><button className="text-button" onClick={() => setView('pricing')}>Ver tarifas<ChevronRight size={14} /></button></div>}
          {view === 'overview' && <div className="breakdown-grid"><Breakdown groups={data?.agents ?? []} type="agent" ready={!!data} /><Breakdown groups={data?.models ?? []} type="model" ready={!!data} /></div>}
          <History data={data} ready={!!data} connect={() => setConnecting(true)} />
        </>}
        {view === 'pricing' && <Pricing />}
        <footer className="main-footer"><span><span className="footer-mark"><Activity size={13} /></span>Visibilidad para cada token.</span><span>{updatedAt ? `Última sincronización ${time(updatedAt)}` : 'Esperando conexión'}<span className="footer-dot">·</span>Almacenamiento local</span></footer>
      </main>
    </div>
    {connecting && <ConnectDialog close={() => setConnecting(false)} />}
  </div>;
}
