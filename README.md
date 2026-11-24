# AI Usage Monitor

[![CI](https://github.com/soyaelithia-ui/ai-usage-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/soyaelithia-ui/ai-usage-monitor/actions/workflows/ci.yml)
[![Licencia: MIT](https://img.shields.io/badge/Licencia-MIT-10b981.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8.svg)](go.mod)
[![Svelte 5](https://img.shields.io/badge/Svelte-5-FF3E00.svg)](web/package.json)
[![Zero CGo](https://img.shields.io/badge/CGo-0%20(Go%20puro)-10b981.svg)](#requisitos)

Panel autoalojado y privado para observar el consumo de agentes de IA en tiempo real. Incluye métricas por respuesta, gráficas por agente y modelo, desglose de caché y razonamiento y una estimación decimal exacta del coste equivalente de API.

Compila en un **único binario Go puro (sin CGo)** que embebe el frontend compilado en **Svelte 5** mediante `go:embed`, persistencia **SQLite WAL** pura en Go (`modernc.org/sqlite`) y actualizaciones en directo por **Server-Sent Events (SSE)**. El panel, las API y el flujo de eventos requieren autenticación por defecto (con modo opcional sin token restringido a red local) y nunca almacenan el contenido de tus conversaciones.

## Funciones principales

- **Binario único autónomo**: backend Go (`CGO_ENABLED=0`) + interfaz Svelte 5 embebida; sin Node.js en producción, sin Python ni gateway separado.
- **Vistas dedicadas**: vista general comparativa, historial desplegable paginado y panel de fuentes/conciliación, solicitando únicamente los datos de la vista activa en cada evento SSE o cambio de filtro.
- **Ventanas y filtros unificados**: 15 minutos, 1 hora, 24 horas y 7 días con filtros compartidos por identificador canónico de agente, modelo, proveedor y sesión.
- **Aritmética decimal exacta**: contadores enteros de precisión arbitraria (`big.Int`) y costes calculados con `shopspring/decimal` (cero punto flotante IEEE 754).
- **Reglas *fail-closed***: modelos desconocidos, depreciados, sin desglose completo de caché, con modalidad no verificada o sin tarifa de escritura de caché (`12h` nunca cae a `30m`) quedan explícitamente sin estimar.
- **Invariante de exposición**: el catálogo interno de tarifas es privado; la web y `/api/pricing` solo exponen modelos realmente observados en telemetría.
- **Autodetección y auto-registro**: descubre localmente 8 tecnologías (`codex`, `opencode`, `claude`, `antigravity`, `gemini`, `cursor`, `copilot`, `hermes`) sin exponer rutas locales y admite autorregistro HTTP y declaración automática de tarifas (`POST /api/ingest`, `POST /api/agents/register`) más resolución opcional contra fuentes públicas verificables (`models.dev`, LiteLLM, OpenRouter) en caché local.
- **Hooks nativos y cuotas en tiempo real**: compatibilidad nativa y **no bloqueante** con los hooks de **Antigravity** (`metadata.user_markdown` con 0 tokens al modelo), **Codex CLI**, **Claude Code** (hooks + `statusLine`), **Gemini CLI**, **Cursor** y **GitHub Copilot CLI** mediante `ai-usage-monitor hook`, instalador idempotente (`ai-usage-monitor hooks install`) y endpoint local `POST /api/hooks/{agent}`.
- **Respaldo interno silencioso**: múltiples tokenizadores ocultos en Go puro (sin CGo) para texto visible sin contadores nativos, guardados aparte para conciliación y nunca sumados al consumo nativo ni expuestos en la UI o API pública.
- **Seguridad perimetral**: token de un solo uso de 8 h por defecto sellado con `machine.key` (HMAC-SHA256) —o tokens permanentes/reutilizables/TTL configurable y modo opcional sin token restringido a red local (`AUTH_MODE=local`)—, cookie `__Host-usage-monitor` (`Secure`, o `usage-monitor` en HTTP plano sobre LAN), CSRF sincronizado, rechazo de cabeceras de proxy/traversals/escáneres y CSP estricta sin estilos inline.
- **Diseño accesible**: 4 temas (`Obsidiana`, `Zafiro`, `Ámbar`, `Esmeralda`) en modo oscuro y claro con contraste WCAG AA, `tabular-nums`, `font-kerning: none` y respeto a `prefers-reduced-motion`.

## Requisitos

- **Go 1.26+** (`CGO_ENABLED=0`).
- **Node.js 22+ y npm** únicamente para compilar los activos embebidos de `web/` y ejecutar pruebas de frontend/E2E.

## Inicio rápido

1. Clona el repositorio:

```sh
git clone https://github.com/soyaelithia-ui/ai-usage-monitor.git
cd ai-usage-monitor
```

2. Compila el frontend Svelte 5 embebido:

```sh
cd web
npm ci
npm run build
cd ..
```

3. Compila el binario único sin CGo:

```sh
CGO_ENABLED=0 go build -o bin/ai-usage-monitor ./cmd/server
```

4. Emite un token de acceso (por defecto de un solo uso, caduca en 8 horas y revoca accesos anteriores):

```sh
./bin/ai-usage-monitor issue
# Opciones adicionales: --permanent, --reusable, --ttl 30d, --keep
```

5. Inicia el servidor en loopback (`http://127.0.0.1:8080`) o en red local:

```sh
./bin/ai-usage-monitor
# En LAN con token: ./bin/ai-usage-monitor --lan
# En LAN sin token (solo IPs locales): ./bin/ai-usage-monitor --lan --local
```

Abre `http://127.0.0.1:8080`, introduce el token en `/access` y accede al panel (en modo `--local` / `--no-token` entra directamente sin pasar por `/access`). Para revocar todos los accesos y cerrar sesiones SSE activas:

```sh
./bin/ai-usage-monitor revoke
```

## Configuración

El binario lee opcionalmente `$XDG_CONFIG_HOME/ai-usage-monitor/backend.json` (o la ruta indicada en `USAGE_MONITOR_CONFIG`) y permite sobrescribir cada opción mediante variables de entorno. Puedes partir de la plantilla [config/backend.example.json](config/backend.example.json):

```sh
mkdir -p ~/.config/ai-usage-monitor
cp config/backend.example.json ~/.config/ai-usage-monitor/backend.json
chmod 600 ~/.config/ai-usage-monitor/backend.json
```

| Variable de entorno         | Clave JSON (`backend.json`) | Descripción                                                                                                               |
| --------------------------- | --------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `USAGE_MONITOR_CONFIG`      | —                           | Ruta al archivo JSON de configuración (por defecto `$XDG_CONFIG_HOME/ai-usage-monitor/backend.json`).                     |
| `PORT`                      | `backendPort`               | Puerto HTTP de escucha (por defecto `8080`).                                                                              |
| `BIND_ADDR`                 | `bindAddr`                  | Dirección IP de escucha (por defecto `127.0.0.1`; cualquier IP no-loopback activa automáticamente `ALLOW_LAN`).           |
| `AUTH_MODE`                 | `authMode`                  | Modo de autenticación: `token` (por defecto) o `local` (acepta `lan`, `no-token`, `local-no-token`; solo IPs de red local). |
| `ALLOW_LAN`                 | `allowLan`                  | Habilita orígenes HTTP de red local/privada y `*.local` (`false` por defecto; automático con `AUTH_MODE=local` o LAN bind). |
| `ACCESS_TOKEN`              | `accessToken`               | Token estático opcional de 43 caracteres base64url (distinto de `gatewayKey` e `ingestKey`, reutilizable y sin caducidad). |
| `SESSION_TTL`               | `sessionTtl`                | Duración de sesión por defecto (p. ej. `24h`, `30d`, `365d`, `2w`); en `ACCESS_TOKEN` por defecto `365d`.                 |
| `DB_PATH`                   | `db`                        | Ruta de la base SQLite WAL (por defecto `./data/usage.db`).                                                               |
| `USAGE_MONITOR_MACHINE_KEY` | —                           | Ruta opcional de `machine.key` (por defecto junto a `DB_PATH`, permisos `0600`).                                          |
| `ORIGIN`                    | `origin`                    | Origen principal (`https://…`, HTTP en loopback, o HTTP en red local cuando `ALLOW_LAN=true`).                            |
| `ALLOWED_ORIGINS`           | `allowedOrigins`            | Orígenes extra permitidos (exactos, comodín de un label `https://*.dominio` o locales si `ALLOW_LAN=true`).              |
| `INGEST_KEY`                | `ingestKey`                 | Secreto base64url de 43 caracteres para `POST /api/ingest`, `POST /api/agents/register` y `POST /api/hooks/{agent}`.      |
| `GATEWAY_KEY`               | `gatewayKey`                | Secreto opcional si un proxy de borde autentica el tráfico hacia el backend.                                              |
| `CODEX_ROOT`                | `codexRoot`                 | Directorio JSONL de Codex.                                                                                                |
| `OPENCODE_DB`               | `openCodeDb`                | Base SQLite de OpenCode 2.1.4.                                                                                            |
| `CLAUDE_ROOT`               | `claudeRoot`                | Directorio JSONL de Claude Code.                                                                                          |
| `ANTIGRAVITY_ROOT`          | `antigravityRoot`           | Directorio local de Antigravity.                                                                                          |
| `GEMINI_ROOT`               | `geminiRoot`                | Directorio JSONL de Gemini CLI.                                                                                           |
| `CURSOR_ROOT`               | `cursorRoot`                | Directorio JSONL de Cursor.                                                                                               |
| `COPILOT_ROOT`              | `copilotRoot`               | Directorio JSONL de GitHub Copilot.                                                                                       |
| `HERMES_ROOT`               | `hermesRoot`                | Directorio JSONL de Hermes Agent.                                                                                         |
| `PRICING_SYNC_ENABLED`      | `pricingSyncEnabled`        | Habilita resolución opcional en red hacia `models.dev`, LiteLLM y OpenRouter (`false` por defecto).                       |
| `PRICING_CACHE_PATH`        | `pricingCachePath`          | Caché local JSON (`0600`) para tarifas externas verificadas.                                                              |
| `PRICING_SOURCES`           | `pricingSources`            | Orden de fuentes externas (`models.dev,litellm,openrouter`).                                                              |
| `QUOTA_WINDOW`              | `quotas.window`             | Ventana de cuota en tiempo real (`15m`, `1h`, `24h`, `7d`; por defecto `24h`).                                            |
| `QUOTA_USD`                 | `quotas.default.usd`        | Límite decimal exacto en USD por ventana (opcional, solo informativo, nunca bloquea).                                     |
| `QUOTA_TOKENS`              | `quotas.default.tokens`     | Límite entero de tokens por ventana (opcional, solo informativo, nunca bloquea).                                          |
| `QUOTAS_JSON`               | `quotas`                    | Configuración JSON de cuotas globales y por agente (`quotas.agents.<id>`).                                                |
| `RATE_LIMIT_MAX`            | —                           | Límite global de peticiones por minuto (por defecto `600`).                                                               |

> [!NOTE]
> Si `INGEST_KEY` no está configurado, el binario genera en memoria un secreto efímero de 256 bits y mantiene cerrados los endpoints de ingesta HTTP hasta que configures una clave explícita.

### Tokens permanentes, reutilizables y caducidad

Por defecto, `./bin/ai-usage-monitor issue` revoca accesos/sesiones anteriores y emite un token de un solo uso válido durante 8 horas. Puedes personalizar la emisión mediante flags o configuración estática:

- `issue --permanent`: genera un token reutilizable que nunca caduca (`expiresAt: "never"`, `permanent: true`, `reusable: true`) y crea sesiones de `365d` por defecto (o la duración indicada en `--ttl`).
- `issue --reusable`: permite múltiples canjes en `/auth/exchange` hasta la caducidad del token.
- `issue --ttl <duración>`: fija la caducidad del token y de la sesión resultante (acepta unidades Go como `24h` y sufijos `d`/`w` como `30d`, `365d`, `2w`). Combinado con `--permanent`, el token no caduca y cada sesión dura `<duración>`.
- `issue --keep`: conserva los tokens y sesiones activas existentes al emitir uno nuevo.
- `ACCESS_TOKEN` / `accessToken` + `SESSION_TTL` / `sessionTtl`: define un token estático de 43 caracteres base64url en configuración/entorno (distinto de `GATEWAY_KEY` e `INGEST_KEY`). Cuando se canjea en `/auth/exchange`, crea una sesión con duración `SESSION_TTL` (o `365d` si no se define). `SESSION_TTL` también sirve como duración de sesión por defecto para tokens emitidos sin `--ttl` ni `--permanent`.

### Modo sin token y acceso en red local (LAN)

Para exponer el panel en tu red local o acceder sin pantalla `/access`:

- **Flags CLI de arranque**:
  - `--lan`: vincula el servidor a `0.0.0.0` (salvo que indiques `--bind <ip>`) y habilita `ALLOW_LAN=true` para aceptar orígenes HTTP privados/locales (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `fc00::/7`, `fe80::/10`, loopback y `*.local` mDNS).
  - `--local` / `--no-token`: activa `AUTH_MODE=local` y `ALLOW_LAN=true` (añade `--lan` o `--bind <ip>` si deseas escuchar fuera de `127.0.0.1`).
  - `--bind <ip>` y `--port <n>`: sobrescriben `BIND_ADDR` y `PORT`.
- **Control estricto por IP local en `AUTH_MODE=local`**: el servidor comprueba `RemoteAddr` en todas las rutas (perímetro HTTP, `/auth/session`, `/auth/logout`, API, SSE y `NotFound`) y rechaza con `403 Forbidden` cualquier IP pública o no local. La protección de `Origin`, `Sec-Fetch-Site: cross-site`, cabeceras de proxy y CSP siguen activas.
- **Comportamiento de cookies en LAN**: en HTTPS y HTTP sobre loopback (`127.0.0.1`, `localhost`, `::1`) se emite `__Host-usage-monitor` (`Secure: true`, `HttpOnly`, `SameSite=Strict`). Cuando `ALLOW_LAN=true` y la petición llega por HTTP plano no-loopback (p. ej. `http://192.168.1.50:8080`), usa `usage-monitor` (`Secure: false`, `HttpOnly`, `SameSite=Strict`) para que el navegador acepte la cookie sin TLS. Consulta [docs/deployment.md](docs/deployment.md) para detalles y advertencias de seguridad.

## Cualquier agente y cualquier modelo

- **Autodetección local**: `internal/ingest/discovery.go` detecta automáticamente (sin seguir enlaces simbólicos ni filtrar rutas locales en la API) Codex, OpenCode, Claude Code, Antigravity, Gemini CLI, Cursor, GitHub Copilot y Hermes Agent.
- **Ingesta por contrato (`POST /api/ingest`)**: cualquier agente autenticado con `Authorization: Bearer <INGEST_KEY>` puede enviar eventos `usage-monitor.usage.v1`, autorregistrarse y declarar tarifas exactas (`rates` o `rate`) para modelos nuevos.
- **Registro explícito (`POST /api/agents/register`)**: permite registrar agentes personalizados con presentación, icono validado y tarifas decimales (los agentes base `codex`, `opencode` y `antigravity` son inmutables).
- **Normalización y prioridad de tarifas**: prefijos de proveedor/agregadores (`openrouter/`, `bedrock/`, `vertex_ai/`, `anthropic/`, `z-ai/`, `moonshotai/`, etc.) y sufijos de fecha/versión se normalizan de forma idéntica en descubrimiento, cálculo de costes y tokenización interna. Precedencia: tarifa declarada por agente > catálogo interno verificado > caché local de fuentes públicas verificables > *fail-closed*.

Ejemplo mínimo de envío por contrato local:

```sh
curl -X POST http://127.0.0.1:8080/api/ingest \
  -H "Authorization: Bearer $INGEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "schema": "usage-monitor.usage.v1",
    "semantics": "input_includes_cache;output_includes_reasoning",
    "source": "mi-agente",
    "provider": "openai",
    "model": "gpt-5.4",
    "responseId": "resp-001",
    "sessionId": "sess-001",
    "occurredAt": "2026-11-20T12:00:00.000Z",
    "inputTokens": 1200,
    "cachedInputTokens": 400,
    "cacheWriteInputTokens": 0,
    "outputTokens": 350,
    "reasoningOutputTokens": 100
  }'
```

## API

| Endpoint                    | Autenticación                     | Función                                                                                                        |
| --------------------------- | --------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `GET /health`               | Pública                           | Comprobación de salud del binario.                                                                             |
| `GET /access`               | Pública                           | Pantalla de acceso para canje de token.                                                                        |
| `GET /favicon.svg`          | Pública                           | Icono SVG de la aplicación (disponible sin autenticación para `/access` y el panel).                            |
| `POST /auth/exchange`       | Pública (limitada)                | Canje de token (único, reutilizable/permanente o `ACCESS_TOKEN`) por cookie + `{authMode, tokenRequired, permanent, csrf, expiresAt}`. |
| `GET /auth/session`         | Cookie de sesión / LAN en `local` | Devuelve `{authMode, tokenRequired, csrf, expiresAt}` (`200` sin cookie para IP local en `AUTH_MODE=local`; `403` si IP externa). |
| `POST /auth/logout`         | Sesión + CSRF / LAN en `local`    | Cierre de sesión y revocación inmediata de SSE (`200 {"ok":true}` en `AUTH_MODE=local` para IP local).         |
| `GET /api/analytics`        | Cookie de sesión                  | Totales, 60 cubos temporales, tarjetas de agentes y desglose por modelo.                                       |
| `GET /api/events`           | Cookie de sesión                  | Historial paginado y opciones de filtro de la ventana seleccionada.                                            |
| `GET /api/sources`          | Cookie de sesión                  | Estado de lectores, agentes activos, hooks nativos e incidencias.                                              |
| `GET /api/hooks`            | Cookie de sesión                  | Integraciones de hooks nativos y estado de cuotas en tiempo real.                                              |
| `GET /api/discovery`        | Cookie de sesión                  | Tecnologías detectadas y modelos observados con estado de tarifa.                                              |
| `GET /api/reconciliation`   | Cookie de sesión                  | Conciliación nativa por sesión y mediciones separadas de texto visible.                                        |
| `GET /api/pricing`          | Cookie de sesión                  | Tarifas resueltas exclusivamente para modelos observados en eventos.                                           |
| `GET /api/stream`           | Cookie de sesión                  | Notificaciones SSE (`ready`, `change`, `expired`).                                                             |
| `POST /api/ingest`          | Bearer `INGEST_KEY`               | Ingesta local y auto-registro de tarifas de modelos.                                                           |
| `POST /api/agents/register` | Bearer `INGEST_KEY`               | Registro explícito de agentes dinámicos y tarifas.                                                             |
| `POST /api/hooks/{agent}`   | Bearer `INGEST_KEY`               | Respuesta de hook nativo no bloqueante (cuota y gasto en tiempo real).                                         |

## Pruebas y desarrollo

Ejecuta la verificación completa (Go sin CGo + frontend Svelte 5 + auditoría E2E en Chromium):

```sh
test -z "$(gofmt -l cmd internal web)"
CGO_ENABLED=0 go vet ./...
(cd web && npm ci && npm run check && npm test && npm run build)
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go build -o bin/ai-usage-monitor ./cmd/server
(cd web && npm run test:e2e)
```

Consulta [docs/testing.md](docs/testing.md) para el detalle de cada suite.

## Seguridad y privacidad

AI Usage Monitor está diseñado como un panel privado autoalojado:

- Escucha por defecto en loopback (`127.0.0.1`, configurable con `BIND_ADDR` / `--lan` y aislamiento estricto de red local en modo `local`) y nunca persiste prompts ni respuestas de conversaciones en su base de datos.
- Bloquea traversals, sondas de escáneres, cabeceras de proxy forjadas, framing y estilos inline (`CSP` estricta).
- Lee registros locales en solo lectura con `O_RDONLY | O_NONBLOCK | O_NOFOLLOW` sin seguir enlaces simbólicos ni bloquear en FIFOs.

Consulta [SECURITY.md](SECURITY.md) para el modelo de amenazas y el canal privado de reporte de vulnerabilidades.

## Documentación adicional

- [Hooks nativos de agentes y cuotas en tiempo real](docs/hooks.md)
- [Medición, tarifas y tokenizadores ocultos](docs/measurement.md)
- [Despliegue con systemd y HTTPS](docs/deployment.md)
- [Pruebas y auditoría E2E](docs/testing.md)
- [Especificación de arquitectura](docs/spec/WAVE1_SPEC.md)
- [Invariantes arquitectónicos](docs/spec/INVARIANTS.md)
- [Modelo de seguridad](SECURITY.md)
- [Sistema de diseño](DESIGN.md)
- [Dependencias, licencias y avisos](NOTICE.md)

## Contribuir

Las contribuciones son bienvenidas. Antes de abrir un *pull request*, revisa la [guía de contribución](CONTRIBUTING.md) y el [código de conducta](CODE_OF_CONDUCT.md).

## Licencia

Distribuido bajo la licencia **MIT**. Consulta [LICENSE](LICENSE) y [NOTICE.md](NOTICE.md) para más información.
