# Medición y formatos

## Fuentes de consumo

| Fuente            | Contrato                                                                | Normalización                                                                                                                      |
| ----------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Codex             | `token_usage_record.payload.usage` y `turn_context` con el mismo turno. | Entrada y salida incluyen los respectivos desgloses. Se ignoran acumulados y eventos redundantes.                                  |
| OpenCode          | `session_v2` y `session_message`, versión 2.1.4.                        | Entrada = normal + caché leída + escrita. Salida = visible + razonamiento. Solo mensajes finalizados y compactaciones completadas. |
| Ingesta declarada | Contrato explícito `usage-monitor.usage.v1` a través de ingesta local.  | Requiere proveedor, modelo y semántica verificables. Los registros binarios sin un esquema validado se mantienen sin interpretar.  |

No se infiere el modelo a partir del texto de una conversación. Los datos ausentes o los esquemas no compatibles generan un estado de espera o una incidencia. Algunos agentes normalizan valores ausentes como cero antes de escribirlos; la cobertura del evento expone esa limitación.

Referencias: [OpenCode 2.1.4](https://github.com/anomalyco/opencode/blob/v2.1.4/packages/core/src/session/usage.ts) y [UsageMetadata](https://ai.google.dev/api/generate-content#UsageMetadata).

## Persistencia

SQLite conserva métricas, identificadores, procedencia, cursores y estado de acceso. No conserva prompts, respuestas ni líneas originales de conversaciones. Los lectores abren las fuentes en modo de lectura. Las transacciones mantienen juntos los eventos y el avance del cursor.

El lector JSONL avanza únicamente sobre líneas completas y detecta rotación o truncamiento mediante inodo, tamaño y firma del tramo anterior. Cada lectura está limitada a 16 MiB. Una línea que supere ese límite queda señalada y requiere revisión.

El lector OpenCode utiliza `readonly` y `query_only`, lotes de 200 registros y una espera de bloqueo máxima de 50 ms. El cursor considera fecha de actualización e identificador.

## Coste equivalente

La entrada normal es la entrada total menos caché leída y escrita. Se calculan por separado entrada normal, caché y salida. El razonamiento es parte de salida. Los valores desconocidos no se valoran como cero.

El catálogo de `server/catalog.js` identifica los modelos admitidos, sus supuestos y los cambios de tarifa programados. Utiliza API Standard como referencia y conserva el precio guardado de cada evento. No se recalcula el historial al editar tarifas. Herramientas, impuestos, descuentos, suscripciones y almacenamiento de caché pueden quedar fuera de esa equivalencia.

Las tarifas cambian; revisa las fuentes oficiales antes de utilizar el resultado para planificación: [OpenAI](https://developers.openai.com/api/docs/pricing) y [Google](https://ai.google.dev/gemini-api/docs/pricing).

## Tokenizadores

Las versiones y hashes de dependencias están fijados en `worker/requirements.lock`, y el catálogo de vocabularios (`worker/vocabularies.json`) declara repositorio, revisión y checksum de cada vocabulario oficial. El worker admite cuatro motores: tiktoken (modelos OpenAI), archivos BPE de tiktoken con su patrón oficial (ChatGLM, Kimi), tokenizadores rápidos de Hugging Face (Qwen, DeepSeek, GLM, Yi, MiniCPM, Llama, Mistral) y SentencePiece (Baichuan). Cada archivo se verifica contra su checksum antes de cargarse y los motores se mantienen en una caché acotada.

El texto necesario para un diagnóstico transita por memoria y un socket Unix. Se limita a 2 MiB por tarea, 32 tareas en cola y ocho segundos por proceso de cálculo. La integración nativa continúa si el tokenizador no está disponible.

Una estimación de texto visible no reconstruye instrucciones internas, llamadas a herramientas, medios ni razonamiento oculto. Se etiqueta como parcial, permanece fuera de los totales de consumo y se descarta si después llega el contador nativo correspondiente. Solo pueden compararse mediciones con la misma cobertura.
