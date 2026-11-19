# AI Usage Monitor

Panel privado para observar el consumo de agentes de IA en tiempo real. Incluye métricas por respuesta, gráficas por agente y modelo, desglose de caché y razonamiento y una estimación exacta del coste equivalente de API.

Compila en un **único binario Go puro (sin CGo)** que embebe el frontend compilado en **Svelte 5** mediante `go:embed`, persistencia **SQLite WAL** pura en Go (`modernc.org/sqlite`) y actualizaciones en directo por **Server-Sent Events (SSE)**. El panel, las API y el flujo de eventos requieren autenticación.

## Funciones

- **Binario único autónomo**: backend Go (`CGO_ENABLED=0`) + interfaz Svelte 5 embebida; sin Node.js en producción, sin Python ni gateway separado.
- **Vistas dedicadas**: vista general comparativa, historial desplegable paginado y panel de fuentes/conciliación, solicitando únicamente los datos de la vista activa en cada evento SSE o cambio de filtro.
- **Ventanas y filtros unificados**: 15 minutos, 1 hora, 24 horas y 7 días con filtros compartidos por identificador canónico de agente, modelo, proveedor y sesión.
- **Aritmética decimal exacta**: contadores enteros de precisión arbitraria (`big.Int`) y costes calculados con `shopspring/decimal` (cero punto flotante).
- **Reglas *fail-closed***: modelos desconocidos, depreciados, sin desglose completo de caché, con modalidad no verificada o sin tarifa de escritura de caché (`12h` nunca cae a `30m`) quedan explícitamente sin estimar.
- **Invariante de exposición**: el catálogo interno de tarifas es privado; la web y `/api/pricing` solo exponen modelos realmente observados en telemetría.
- **Autodetección y auto-registro**: descubre localmente 8 tecnologías (`codex`, `opencode`, `claude`, `antigravity`, `gemini`, `cursor`, `copilot`, `hermes`) y admite autorregistro HTTP y declaración automática de tarifas (`POST /api/ingest`, `POST /api/agents/register`) más resolución opcional contra fuentes verificables (`models.dev`, LiteLLM, OpenRouter) en caché local.
- **Respaldo interno silencioso**: múltiples tokenizadores ocultos en Go puro (sin CGo) para texto visible sin contadores nativos, guardados aparte para conciliación y nunca sumados al consumo nativo ni expuestos en la UI o API pública.
- **Seguridad perimetral**: token de un solo uso sellado con `machine.key` (HMAC-SHA256), sesión `__Host-usage-monitor` de 8 h, CSRF sincronizado, rechazo de cabeceras de proxy/traversals/escáneres y CSP estricta sin estilos inline.
- **Diseño accesible**: 4 temas (`Obsidiana`, `Zafiro`, `Ámbar`, `Esmeralda`) en modo oscuro y claro con contraste WCAG AA, `tabular-nums`, `font-kerning: none` y respeto a `prefers-reduced-motion`.

## Requisitos

- **Go 1.26+** (`CGO_ENABLED=0`).
- **Node.js 22+ y npm** únicamente para compilar los activos embebidos de `web/` y ejecutar pruebas de frontend/E2E.

## Inicio rápido

1. Compila el frontend Svelte 5 embebido:

```sh
cd web
npm ci
npm run build
cd ..
```

2. Compila el binario único sin CGo:

```sh
CGO_ENABLED=0 go build -o bin/ai-usage-monitor ./cmd/server
```

3. Emite un token de acceso de un solo uso (caduca en 8 horas y revoca accesos anteriores):

```sh
./bin/ai-usage-monitor issue
```

4. Inicia el servidor en loopback (`http://127.0.0.1:8080`):

```sh
./bin/ai-usage-monitor
```

Abre `http://127.0.0.1:8080`, introduce el token en `/access` y accede al panel. Para revocar todos los accesos y cerrar sesiones SSE activas:

```sh
./bin/ai-usage-monitor revoke
```

## Configuración

El binario lee opcionalmente `$XDG_CONFIG_HOME/ai-usage-monitor/backend.json` (o la ruta indicada en `USAGE_MONITOR_CONFIG`) y permite sobrescribir cada opción mediante variables de entorno:

| Variable de entorno    | Clave JSON (`backend.json`) | Descripción                                                                                      |
| ---------------------- | --------------------------- | ------------------------------------------------------------------------------------------------ |
| `PORT`                 | `backendPort`               | Puerto loopback local (por defecto `8080`).                                                      |
| `DB_PATH`              | `db`                        | Ruta de la base SQLite WAL (por defecto `./data/usage.db`).                                      |
| `USAGE_MONITOR_MACHINE_KEY` | —                      | Ruta opcional de `machine.key` (por defecto junto a `DB_PATH`, permisos `0600`).                 |
| `ORIGIN`               | `origin`                    | Origen principal (`https://…` o HTTP en loopback).                                               |
| `ALLOWED_ORIGINS`      | `allowedOrigins`            | Orígenes extra permitidos (exactos o comodín de un label `https://*.dominio`).                   |
| `INGEST_KEY`           | `ingestKey`                 | Secreto base64url de 43 caracteres para `POST /api/ingest` y `POST /api/agents/register`.        |
| `GATEWAY_KEY`          | `gatewayKey`                | Secreto opcional si un proxy de borde autentica el tráfico hacia el backend.                     |
| `CODEX_ROOT`           | `codexRoot`                 | Directorio JSONL de Codex.                                                                       |
| `OPENCODE_DB`          | `openCodeDb`                | Base SQLite de OpenCode 2.1.4.                                                                   |
| `CLAUDE_ROOT`          | `claudeRoot`                | Directorio JSONL de Claude Code.                                                                 |
| `ANTIGRAVITY_ROOT`     | `antigravityRoot`           | Directorio local de Antigravity.                                                                 |
| `GEMINI_ROOT`          | `geminiRoot`                | Directorio JSONL de Gemini CLI.                                                                  |
| `CURSOR_ROOT`          | `cursorRoot`                | Directorio JSONL de Cursor.                                                                      |
| `COPILOT_ROOT`         | `copilotRoot`               | Directorio JSONL de GitHub Copilot.                                                              |
| `HERMES_ROOT`          | `hermesRoot`                | Directorio JSONL de Hermes Agent.                                                                |
| `PRICING_SYNC_ENABLED` | `pricingSyncEnabled`        | Habilita resolución opcional en red hacia `models.dev`, LiteLLM y OpenRouter (`false` por defecto). |
| `PRICING_CACHE_PATH`   | `pricingCachePath`          | Caché local JSON (`0600`) para tarifas externas verificadas.                                     |
| `PRICING_SOURCES`      | `pricingSources`            | Orden de fuentes externas (`models.dev,litellm,openrouter`).                                     |
| `RATE_LIMIT_MAX`       | —                           | Límite global de peticiones por minuto (por defecto `600`).                                      |

## Cualquier agente y cualquier modelo

- **Autodetección local**: `internal/ingest/discovery.go` detecta automáticamente (sin seguir enlaces simbólicos ni filtrar rutas locales en la API) Codex, OpenCode, Claude Code, Antigravity, Gemini CLI, Cursor, GitHub Copilot y Hermes Agent.
- **Ingesta por contrato (`POST /api/ingest`)**: cualquier agente autenticado con `Authorization: Bearer <INGEST_KEY>` puede enviar eventos `usage-monitor.usage.v1`, autorregistrarse y declarar tarifas exactas (`rates` o `rate`) para modelos nuevos.
- **Registro explícito (`POST /api/agents/register`)**: permite registrar agentes personalizados con presentación, icono validado y tarifas decimales (los agentes base `codex`, `opencode` y `antigravity` son inmutables).
- **Normalización y prioridad de tarifas**: prefijos de proveedor/agregadores (`openrouter/`, `bedrock/`, `vertex_ai/`, `anthropic/`, `z-ai/`, `moonshotai/`, etc.) y sufijos de fecha/versión se normalizan de forma idéntica en descubrimiento, cálculo de costes y tokenización interna. Precedencia: tarifa declarada por agente > catálogo interno verificado (noviembre 2026) > caché local de fuentes públicas verificables > *fail-closed*.

## API

| Endpoint                     | Autenticación      | Función                                                                |
| ---------------------------- | ------------------ | ---------------------------------------------------------------------- |
| `GET /health`                | Pública            | Comprobación de salud del binario.                                     |
| `GET /access`                | Pública            | Pantalla de acceso para canje de token.                                |
| `POST /auth/exchange`        | Pública (limitada) | Canje atómico de token de un solo uso por sesión + CSRF.               |
| `GET /auth/session`          | Cookie de sesión   | Estado y caducidad de la sesión vigente.                               |
| `POST /auth/logout`          | Sesión + CSRF      | Cierre de sesión y revocación inmediata de SSE.                        |
| `GET /api/analytics`         | Cookie de sesión   | Totales, 60 cubos temporales, tarjetas de agentes y desglose por modelo. |
| `GET /api/events`            | Cookie de sesión   | Historial paginado y opciones de filtro de la ventana seleccionada.    |
| `GET /api/sources`           | Cookie de sesión   | Estado de lectores, agentes activos e incidencias recientes.           |
| `GET /api/discovery`         | Cookie de sesión   | Tecnologías detectadas y modelos observados con estado de tarifa.      |
| `GET /api/reconciliation`    | Cookie de sesión   | Conciliación nativa por sesión y mediciones separadas de texto visible. |
| `GET /api/pricing`           | Cookie de sesión   | Tarifas resueltas exclusivamente para modelos observados en eventos.   |
| `GET /api/stream`            | Cookie de sesión   | Notificaciones SSE (`ready`, `change`, `expired`).                     |
| `POST /api/ingest`           | Bearer `INGEST_KEY`| Ingesta local y auto-registro de tarifas de modelos.                   |
| `POST /api/agents/register`  | Bearer `INGEST_KEY`| Registro explícito de agentes dinámicos y tarifas.                     |

## Documentación adicional

- [Medición, tarifas y tokenizadores ocultos](docs/measurement.md)
- [Despliegue con systemd y HTTPS](docs/deployment.md)
- [Pruebas y auditoría E2E](docs/testing.md)
- [Modelo de seguridad](SECURITY.md)
- [Sistema de diseño](DESIGN.md)
- [Dependencias y avisos](NOTICE.md)
