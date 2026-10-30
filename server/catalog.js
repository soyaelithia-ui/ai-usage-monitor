/**
 * Declarative catalogue of agents, models and prices.
 *
 * There are no per-agent branches anywhere else in the backend: readers, API
 * and dashboard consume this data. Supporting another agent means adding an
 * entry here (or declaring it in the configuration), never editing logic.
 *
 * Reader kinds
 *   - `jsonl`   : one JSON object per line, under `path`.
 *   - `sqlite`  : an agent-owned SQLite database.
 *   - `contract`: the agent pushes its own metrics through the ingestion API.
 *
 * `usage` maps semantic fields to dotted paths inside the parsed record. A
 * list of paths means those values are added together and a missing value
 * counts as zero; the first path is the primary counter and must be present.
 * Optional fields stay unknown instead of being defaulted to zero.
 */
export const agents = [
  {
    id: 'codex',
    name: 'Codex',
    reader: 'jsonl',
    source: 'codex',
    provider: 'openai',
    tone: 'mint',
    symbol: 'C',
    configKey: 'codexRoot',
    filePattern: '\\.jsonl(?:\\.[\\w-]+)?$',
    markers: ['"turn_context"', '"token_usage_record"'],
    recordTypes: ['token_usage_record'],
    context: { recordType: 'turn_context', key: 'payload.turn_id', model: 'payload.model' },
    usage: {
      responseId: 'payload.response_id',
      sessionId: ['payload.session_id', 'payload.thread_id'],
      turnId: 'payload.turn_id',
      model: 'context.model',
      occurredAt: 'timestamp',
      inputTokens: 'payload.usage.input_tokens',
      outputTokens: 'payload.usage.output_tokens',
      cachedInputTokens: 'payload.usage.cached_input_tokens',
      cacheWriteInputTokens: 'payload.usage.cache_write_input_tokens',
      reasoningOutputTokens: 'payload.usage.reasoning_output_tokens',
    },
    method: 'native',
    sourceVersion: 'codex.token_usage_record.v1',
    coverage: 'provider_response_usage',
  },
  {
    id: 'opencode',
    name: 'OpenCode',
    reader: 'sqlite',
    source: 'opencode',
    tone: 'blue',
    symbol: 'O',
    configKey: 'openCodeDb',
    expectedVersion: '2.1.4',
    query: {
      sql: "SELECT m.id,m.session_id,m.time_updated,m.data,s.version FROM session_message m JOIN session_v2 s ON s.id=m.session_id WHERE m.type IN ('assistant','compaction') AND (m.time_updated>? OR (m.time_updated=? AND m.id>?)) ORDER BY m.time_updated,m.id LIMIT 200",
      cursor: ['time_updated', 'id'],
      columns: {
        id: 'id',
        sessionId: 'session_id',
        updated: 'time_updated',
        version: 'version',
        payload: 'data',
      },
    },
    require: ['model', 'provider'],
    accept: [
      { type: 'assistant', path: 'time.completed' },
      { type: 'compaction', path: 'status', equals: 'completed' },
    ],
    usage: {
      responseId: 'id',
      sessionId: 'context.sessionId',
      occurredAt: 'time.completed',
      occurredAtFallback: 'time.created',
      occurredAtFormat: 'epoch-ms',
      model: 'model.id',
      provider: 'model.providerID',
      inputTokens: ['tokens.input', 'tokens.cache.read', 'tokens.cache.write'],
      outputTokens: ['tokens.output', 'tokens.reasoning'],
      cachedInputTokens: 'tokens.cache.read',
      cacheWriteInputTokens: 'tokens.cache.write',
      reasoningOutputTokens: 'tokens.reasoning',
    },
    fallback: { contentPath: 'content' },
    method: 'native',
    sourceVersion: 'opencode.2.1.4',
    coverage: 'agent_normalized_usage_zero_defaults_possible',
  },
  {
    id: 'antigravity',
    name: 'Antigravity',
    reader: 'contract',
    source: 'antigravity',
    tone: 'violet',
    symbol: 'A',
    usage: {},
    method: 'reported',
    sourceVersion: 'usage-monitor.usage.v1',
    coverage: 'reported_usage',
  },
];

/** Prices in USD per million tokens. `cacheWrite` is absent when unknown. */
export const pricing = {
  'gpt-6-astra': {
    provider: 'openai',
    input: '10',
    cached: '1',
    cacheWrite: '12.5',
    cacheWrite12h: '25',
    output: '50',
  },
  'gpt-6.1-sol': {
    provider: 'openai',
    input: '2',
    cached: '.1',
    cacheWrite: '2.5',
    cacheWrite12h: '5',
    output: '10',
  },
  'gpt-6.1-terra': {
    provider: 'openai',
    input: '1',
    cached: '.05',
    cacheWrite: '1.25',
    cacheWrite12h: '2.5',
    output: '5',
  },
  'gpt-6-luna': {
    provider: 'openai',
    input: '.1',
    cached: '.01',
    cacheWrite: '.125',
    cacheWrite12h: '.25',
    output: '.5',
  },
  'gpt-6-sol': {
    provider: 'openai',
    input: '2',
    cached: '.2',
    cacheWrite: '2.5',
    cacheWrite12h: '5',
    output: '10',
  },
  'gemini-2.5-pro': { provider: 'google', input: '1.25', cached: '.125', output: '10' },
  'gemini-2.5-flash': {
    provider: 'google',
    input: '.30',
    cached: '.03',
    output: '2.50',
    modality: 'text',
  },
  'gemini-2.5-flash-lite': {
    provider: 'google',
    input: '.10',
    cached: '.01',
    output: '.40',
    modality: 'text',
  },
  'gemini-4-argon': { provider: 'google', input: '2', cached: '.10', output: '10' },
  'gemini-3.8-flash': { provider: 'google', input: '.75', cached: '.075', output: '3.75' },
};

/** Long-context multipliers applied above `threshold` input tokens. */
export const longContext = {
  'gpt-6-astra': { threshold: 272000, input: '2', output: '1.5' },
  'gpt-6.1-sol': { threshold: 272000, input: '2', output: '1.5' },
  'gpt-6.1-terra': { threshold: 272000, input: '2', output: '1.5' },
  'gpt-6-luna': { threshold: 272000, input: '2', output: '1.5' },
  'gpt-6-sol': { threshold: 272000, input: '2', output: '1.5' },
  'gemini-2.5-pro': { threshold: 200000, input: '2', output: '1.5' },
};

/** Scheduled price changes; the latest entry whose date has passed wins. */
export const pricingSchedule = {
  'gemini-4-argon': [{ from: '2026-12-01T00:00:00Z', input: '4', cached: '.20', output: '20' }],
  'gemini-3.8-flash': [
    { from: '2027-01-01T00:00:00Z', input: '1.50', cached: '.15', output: '7.50' },
  ],
};

export const pricingNote =
  'Equivalente API Standard, USD; nivel supuesto, no cargo real. El contexto se cuenta en cada llamada, incluso cuando se reutiliza. Tarifas consultadas el 29-10-2026; no representa la factura de ChatGPT/Codex. Caché escrita sin duración: se supone 30 min. Sin recargos regionales ni herramientas. OpenAI: contexto >272.000, entrada/caché ×2 y salida ×1,5. Google Pro 2.5: umbral 200.000; Flash con modalidad desconocida queda sin estimar. Google excluye almacenamiento y herramientas.';

/** Reasons a model can stay unestimated, and the assumed-terms notes. */
export const pricingRules = {
  unknownRate: 'Tarifa no verificada',
  incompleteCache: 'Desglose de caché incompleto',
  modalityUnverified: 'Modalidad de entrada no verificada: la tarifa difiere por modalidad',
  cacheWriteUnavailable: 'Escritura/almacenamiento de caché sin tarifa verificada',
  cacheWriteAssumed: 'Escritura de caché: 30 min supuestos',
  assumedStandard: 'API Standard estimada',
};

/** Extra assumptions per provider, shown when a cost is estimated. */
export const providerNotes = {
  google: 'API Standard supuesta; no incluye almacenamiento de caché, herramientas ni suscripción',
};

/** Palette used when an agent is declared without a tone. */
export const tones = ['mint', 'violet', 'blue', 'amber', 'rose', 'cyan', 'lime', 'sand'];

/** Contract accepted by agents that push their own metrics. */
export const ingestContract = {
  schema: 'usage-monitor.usage.v1',
  semantics: 'input_includes_cache;output_includes_reasoning',
};
