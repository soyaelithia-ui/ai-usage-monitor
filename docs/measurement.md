# Medición, Tarifas y Formatos

## Fuentes de consumo

| Fuente             | Lector / Contrato                                                        | Normalización                                                                                                                      |
| ------------------ | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| Codex              | JSONL (`token_usage_record` + `turn_context`).                           | Entrada y salida incluyen sus desgloses. Detección de reescrituras del mismo tamaño por huella SHA-256 de cola (4096 B).           |
| OpenCode           | SQLite en solo lectura (`session_v2`, `session_message`, v2.1.4).        | Entrada = normal + caché leída + escrita; salida = visible + razonamiento. Solo mensajes completados y compactaciones finalizadas. |
| Claude Code        | JSONL (`message.usage`).                                                 | Entrada = `input_tokens + cache_read + cache_creation`; salida = `output_tokens`.                                                  |
| Gemini CLI         | JSONL (`usageMetadata` o `usage`).                                       | Soporta `promptTokenCount`, `candidatesTokenCount`, `cachedContentTokenCount`, `thoughtsTokenCount`.                               |
| Cursor / Copilot / Hermes | JSONL pasivo o ingesta HTTP `usage-monitor.usage.v1`.             | Normalización unificada por ID canónico de agente; autorregistro automático sin reiniciar.                                         |
| Antigravity / otros| Contrato HTTP local `usage-monitor.usage.v1` (`POST /api/ingest`).       | Requiere proveedor, modelo y semántica `input_includes_cache;output_includes_reasoning`.                                           |

No se infiere el modelo a partir del contenido de la conversación. Las lecturas locales usan `O_RDONLY | O_NONBLOCK | O_NOFOLLOW`, rechazan enlaces simbólicos y FIFOs, limitan profundidad (`MAX_DEPTH = 5`) y observan cambios con `fsnotify` más un respaldo de 60 s.

## Persistencia

SQLite WAL (`modernc.org/sqlite`, Go puro sin CGo) almacena exclusivamente métricas agregables, identificadores canónicos en cadena, cursores, errores y estado de sesiones selladas. Nunca persiste prompts ni respuestas originales de conversaciones.

## Coste equivalente y normalización

Todos los cálculos monetarios usan `github.com/shopspring/decimal` y enteros `math/big` (cero punto flotante IEEE 754):

- **Entrada normal**: `inputTokens - cachedInputTokens - cacheWriteInputTokens`.
- **Razonamiento**: subconjunto de `outputTokens` (no se vuelve a sumar).
- **Normalización canónica (`internal/pricing/normalize.go`)**: retira prefijos de agregadores (`openrouter/`, `litellm/`, `bedrock/`, `vertex_ai/`, `azure/`, `github-copilot/`, `models/`, etc.), prefijos de proveedor directo (`openai/`, `anthropic/`, `google/`, `z-ai/`, `moonshotai/`, `mistralai/`, `x-ai/`, etc.), rutas regionales Bedrock (`us.anthropic.*-v1:0`), puntos/guiones en Claude (`claude-sonnet-4.6` ↔ `claude-sonnet-4-6`) y sufijos de fecha/snapshot (`-YYYY-MM-DD`, `-YYYYMMDD`, `@YYYYMMDD`, `-latest`), preservando variantes `:free`.
- **Precedencia de tarifas**:
  1. Tarifas declaradas explícitamente por el agente (`dynamic_pricing`, `source='agent'`).
  2. Catálogo interno privado verificado a **19 de noviembre de 2026** (`internal/pricing/catalog.go`, consultando tarifas oficiales de OpenAI, Anthropic, Google AI, DeepSeek, Mistral, xAI, Z.AI, Moonshot, MiniMax, Alibaba Qwen, Meta, StepFun y Meituan). Los modelos depreciados (`gpt-5`, `gpt-5-mini`, `gpt-4o`, `gpt-4.1`, `o1`, `o3`, `o3-mini`, `o4-mini`, `gemini-2.5-pro`, `gemini-2.5-flash`, `deepseek-v3`, `deepseek-r1`, etc.) se retiran del catálogo activo y fallan cerrado salvo tarifa explícita declarada por el agente.
  3. Resolución opcional contra fuentes públicas verificables (`models.dev`, LiteLLM, OpenRouter) almacenadas en caché local (`pricing-cache.json`, modo `0600`) con conversión decimal exacta.
  4. **Fail-closed**: si un modelo o componente no puede verificarse (desconocido, depreciado, proveedor incompatible, caché incompleta, modalidad no verificada o escritura de caché `12h` sin tarifa `cacheWrite12h`), `costUsd` queda en `null` con su motivo explícito.
- **Invariante de exposición**: el catálogo interno y la caché externa nunca se publican como lista fija; `/api/pricing` y `/api/discovery` derivan sus modelos exclusivamente de `SELECT DISTINCT model FROM events`.

## Tokenizadores múltiples ocultos (Go puro, sin CGo)

El paquete interno `internal/tokenizer` implementa en Go puro (sin CGo, sin Python y sin acceso de red en ejecución) selección automática por familia de modelo y proveedor para:

- OpenAI (`o200k_base` y `cl100k_base`)
- Anthropic / Claude
- Google / Gemini (SentencePiece)
- Mistral (Tekken para generaciones modernas y SentencePiece para generaciones v0.1–v0.3/Mixtral)
- MiniMax
- Z.AI GLM
- DeepSeek
- Qwen
- Kimi (Moonshot)
- Llama / Grok

Se utilizan **únicamente como respaldo silencioso** cuando un lector local recibe texto visible sin contadores nativos (con límite de 2 MiB por registro y rechazo fail-closed `"unsupported"` para familias desconocidas). Las mediciones resultantes se guardan en la tabla separada `text_measurements` para conciliación, se descartan automáticamente si llega el contador nativo, **nunca se suman al consumo ni coste nativo**, y no exponen pestañas, selectores ni estados de tokenizador en la web ni en la API pública.
