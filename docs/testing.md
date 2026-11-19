# Pruebas y Validación

Todas las comprobaciones se ejecutan sin CGo (`CGO_ENABLED=0`) y con bases de datos temporales sintéticas.

## 1. Formato y análisis estático Go

```sh
test -z "$(gofmt -l cmd internal web)"
CGO_ENABLED=0 go vet ./...
```

## 2. Tipado, pruebas unitarias y compilación Svelte 5

```sh
cd web
npm ci
npm run check
npm test
npm run build
cd ..
```

Cubre formateadores exactos (`BigInt`, moneda), los 4 temas en modo claro/oscuro, construcción de filtros compartidos, carga acotada por vista activa (`requiredEndpointsForTab`) y unificación de identificadores de agentes (`buildAgentOptions`).

## 3. Pruebas unitarias e integración Go (cero CGo)

```sh
CGO_ENABLED=0 go test -count=1 -v ./...
```

Cubre:
- Seguridad perimetral (`internal/security`): traversals literales/codificados, sondas de escáneres, cabeceras de proxy/smuggling, política de origen y comodines, framing, CSP, `SanitizeIconURL` y ciclo de vida de `machine.key`.
- Tarifas decimales y normalización (`internal/pricing`): aritmética exacta con enteros >2⁵³, reglas *fail-closed* (incluyendo `12h` y modalidad), deprecación de modelos retirados, normalización de prefijos/agregadores/fechas/Bedrock/Claude y resolución en caché local (`models.dev`, LiteLLM, OpenRouter).
- Tokenizadores ocultos en Go puro (`internal/tokenizer`): selección automática en las 10 familias, fail-closed para modelos desconocidos, límite 2 MiB y carga de tablas BPE/HF/SentencePiece.
- Ingesta pasiva y autodetección (`internal/ingest`): reescrituras del mismo tamaño, offset de líneas en blanco, seguridad contra symlinks/FIFOs, `fsnotify`, lectura pasiva SQLite sin crear `-shm`, respaldo silencioso en `text_measurements` y autodetección de las 8 tecnologías locales.
- API, autenticación y SSE (`internal/api`): canje atómico de token, CSRF, revocación inmediata de SSE, exposición estricta de modelos observados en `/api/pricing`, ausencia de estado de tokenizador en `/api/sources` y `/api/reconciliation`, auto-registro de agentes/tarifas y filtro unificado por agente.
- Activos embebidos (`web`): verificación de `go:embed` e invariantes CSS.

## 4. Compilación del binario único y auditoría E2E en navegador real

```sh
CGO_ENABLED=0 go build -o bin/ai-usage-monitor ./cmd/server
cd web
npm run test:e2e
```

La suite [web/e2e/browser.e2e.mjs](../web/e2e/browser.e2e.mjs) lanza el binario compilado en un puerto efímero y audita en Chromium real (escritorio `1440×900` y móvil `390×844`):
- Flujo de autenticación `/access`, SSE en directo sin recarga, cierre de sesión y revocación en vivo desde CLI (`revoke`).
- Contraste WCAG AA en las 8 combinaciones (4 temas × oscuro/claro) y persistencia anti-FOUC.
- Cero estilos inline (`[style]`), cero violaciones CSP, `tabular-nums`, `font-kerning: none`, foco visible por teclado y `prefers-reduced-motion: reduce`.
- Peticiones restringidas exclusivamente a la vista activa en cambios de filtro, paginación y eventos SSE.
- Semántica ARIA del historial desplegable (`ul.history-list > li.history-item > details.event-disclosure`) e identificador de agente unificado entre barra lateral y vistas.
- Ausencia total de controles/estados de tokenizador en UI y API.
