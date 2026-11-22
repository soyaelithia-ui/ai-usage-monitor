# Hooks nativos de agentes y cuotas en tiempo real

**AI Usage Monitor** incluye compatibilidad nativa con los sistemas de *lifecycle hooks* (y `statusLine`) de los principales agentes de IA para mostrar **cuota restante y gasto en tiempo real** directamente dentro del propio agente, usando el mismo binario Go autónomo sin dependencias externas (`jq`, Node.js ni Python).

## Garantía no bloqueante (*non-interrupting*)

Todos los adaptadores nativos están diseñados exclusivamente para observabilidad y cumplen invariantes estrictos:

- **Nunca bloquean ni detienen turnos**: jamás emiten `decision: "deny"`, `decision: "block"`, `continue: false`, `terminationBehavior: "terminate"`, `decision: "continue"` en `Stop` ni códigos de salida `2`.
- **Cero contaminación del contexto del modelo**: en Antigravity se utiliza `metadata.user_markdown` con `"systemMessage": ""` (0 tokens enviados al modelo); en Codex, Claude Code, Gemini CLI y Copilot se usa `systemMessage` sin `additionalContext`; en Cursor se usa `user_message` sin `agent_message` ni `followup_message`.
- **Lectura concurrente sin contención**: `ai-usage-monitor hook` abre SQLite en modo solo lectura (`mode=ro`, `busy_timeout=500ms`) para leer WAL en pocos milisegundos sin competir con el servidor en ejecución.
- **Degradación segura (*fail-open*)**: ante `stdin` vacío, malformado o base de datos aún inexistente, el comando siempre devuelve una salida válida y termina con código `0`.

## Agentes compatibles

| Agente | ID | Archivo de proyecto | Archivo global | Eventos instalados | Canal nativo |
| --- | --- | --- | --- | --- | --- |
| **Antigravity** | `antigravity` | `.agents/hooks.json` | `~/.gemini/config/hooks.json` | `SessionStart`, `PostInvocation` | `injectSteps[].systemMessage.metadata.user_markdown` (`systemMessage: ""`) |
| **Codex CLI** | `codex` | `.codex/hooks.json` | `~/.codex/hooks.json` | `SessionStart`, `Stop` | `systemMessage` |
| **Claude Code** | `claude` | `.claude/settings.json` | `~/.claude/settings.json` | `SessionStart`, `Stop` | `continue: true`, `systemMessage` |
| **Claude Code StatusLine** | `claude-statusline` | `.claude/settings.json` | `~/.claude/settings.json` | `statusLine` | Barra de estado persistente (1 línea) |
| **Gemini CLI** | `gemini` | `.gemini/settings.json` | `~/.gemini/settings.json` | `SessionStart`, `AfterAgent` | `systemMessage` |
| **Cursor** | `cursor` | `.cursor/hooks.json` | `~/.cursor/hooks.json` | `afterAgentResponse`, `stop` | `user_message` |
| **GitHub Copilot CLI** | `copilot` | `.github/hooks/ai-usage-monitor.json` | `~/.copilot/hooks/ai-usage-monitor.json` | `postToolUse`, `sessionEnd` | `continue: true`, `systemMessage` |
| **Genérico / OpenCode / Hermes** | `generic` / `statusline` | — | — | Cualquiera | JSON (`systemMessage` + `telemetry`) o línea de texto |

## Instalación rápida en un comando

El instalador fusiona de forma idempotente únicamente las entradas de `ai-usage-monitor`, preservando todos tus hooks y ajustes existentes (y rechazando enlaces simbólicos):

```sh
# Listar todos los adaptadores soportados
./bin/ai-usage-monitor hooks list

# Instalar en el workspace actual (por ejemplo Antigravity o Codex)
./bin/ai-usage-monitor hooks install --agent antigravity
./bin/ai-usage-monitor hooks install --agent codex
./bin/ai-usage-monitor hooks install --agent claude-statusline

# Instalar globalmente para todos los agentes detectados en tu máquina
./bin/ai-usage-monitor hooks install --agent all --scope global

# Vista previa sin modificar archivos
./bin/ai-usage-monitor hooks install --agent antigravity --dry-run

# Imprimir solo el JSON nativo listo para copiar
./bin/ai-usage-monitor hooks print --agent antigravity

# Desinstalar limpiamente (conserva el resto de tus hooks)
./bin/ai-usage-monitor hooks uninstall --agent antigravity
```

## Configurar cuotas decimales exactas

Las cuotas se evalúan con aritmética decimal exacta (`shopspring/decimal`) para USD y enteros de precisión arbitraria (`big.Int`) para tokens. Nunca bloquean la ejecución: al alcanzar el 80 % muestran `[CUOTA ALTA]` y al 100 % `[CUOTA SUPERADA]`.

En `~/.config/ai-usage-monitor/backend.json`:

```json
{
  "quotas": {
    "window": "24h",
    "default": {
      "usd": "10.00",
      "tokens": "5000000"
    },
    "agents": {
      "antigravity": { "usd": "5.00", "window": "24h" },
      "codex": { "usd": "15.00", "window": "7d" },
      "claude": { "usd": "10.00", "window": "24h" }
    }
  }
}
```

También puedes sobrescribirlas mediante variables de entorno (`QUOTA_WINDOW`, `QUOTA_USD`, `QUOTA_TOKENS`, `QUOTAS_JSON`) o flags por invocación:

```sh
./bin/ai-usage-monitor hook --agent antigravity --window 24h --quota-usd 5.00
```

## Comprobar estado y simular un hook

```sh
# Estado agregado de cuotas e instalaciones
./bin/ai-usage-monitor hooks status

# Probar el contrato nativo de Antigravity (devuelve injectSteps con metadata.user_markdown)
echo '{"conversationId":"conv-1","hookEventName":"PostInvocation"}' \
  | ./bin/ai-usage-monitor hook --agent antigravity

# Probar el contrato nativo de Codex CLI
echo '{"session_id":"sess-1","hook_event_name":"Stop","model":"gpt-5.4"}' \
  | ./bin/ai-usage-monitor hook --agent codex

# Modo barra de estado compacta (Claude statusLine, tmux o prompt)
./bin/ai-usage-monitor hook --agent statusline
```

Si ejecutas hooks sin mantener levantado el servidor web de fondo, añade `--sync` al comando para escanear los logs locales antes de responder.

## Hooks por HTTP local (`POST /api/hooks/{agent}`)

Para agentes que soportan hooks HTTP, el servidor expone `POST /api/hooks/{agent}` en loopback protegido con `Authorization: Bearer <INGEST_KEY>` (mismo perímetro local que `/api/ingest`):

```sh
curl -X POST http://127.0.0.1:8080/api/hooks/claude \
  -H "Authorization: Bearer $INGEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"sess-1","hook_event_name":"Stop"}'
```

El panel web también consulta el estado de integraciones y cuotas mediante `GET /api/hooks` (protegido con cookie de sesión y sin exponer rutas locales) y lo muestra en la pestaña **Fuentes y lectores**.
