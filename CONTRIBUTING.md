# Guía de contribución

¡Gracias por tu interés en contribuir a **AI Usage Monitor**! Este documento resume cómo preparar el entorno local, qué invariantes protege el proyecto y cómo enviar cambios de forma segura.

## Antes de empezar

- Revisa las incidencias (*issues*) y *pull requests* abiertos para evitar trabajo duplicado.
- Para cambios arquitectónicos o nuevas funcionalidades grandes, abre primero una incidencia describiendo el problema y la propuesta.
- **Privacidad ante todo**: nunca incluyas en incidencias, commits ni capturas datos reales de sesiones, tokens, claves (`machine.key`, `INGEST_KEY`, `GATEWAY_KEY`), archivos `.db`, cachés locales ni registros reales de tus agentes. Utiliza siempre datos y *fixtures* sintéticos.
- Para vulnerabilidades de seguridad, **no abras una incidencia pública**; sigue las instrucciones de [SECURITY.md](SECURITY.md).

## Preparación del entorno

Requisitos:

- **Go 1.26+** (todas las compilaciones y pruebas deben funcionar con `CGO_ENABLED=0`).
- **Node.js 22+** y **npm** (solo para `web/` y pruebas E2E).

Instalación inicial:

```sh
git clone https://github.com/soyaelithia-ui/ai-usage-monitor.git
cd ai-usage-monitor
cd web && npm ci && npm run build && cd ..
CGO_ENABLED=0 go test ./...
```

## Invariantes del proyecto

Todo cambio debe preservar los contratos documentados en [docs/spec/INVARIANTS.md](docs/spec/INVARIANTS.md):

1. **Binario único sin CGo**: no introduzcas dependencias que requieran CGo ni procesos auxiliares obligatorios en producción.
2. **Aritmética exacta y *fail-closed***: cero punto flotante (`float64`) en cálculos de tarifas o costes; cualquier tarifa o componente no verificable debe devolverse como no estimado (`null`) con motivo explícito.
3. **Invariante de exposición**: el catálogo interno de tarifas es privado; la UI y `/api/pricing` solo exponen modelos realmente observados en eventos.
4. **Tokenización interna oculta**: `internal/tokenizer` es exclusivamente un respaldo silencioso para conciliación en `text_measurements`; no debe añadir pestañas, controles ni estados de tokenizador en la UI ni en la API pública.
5. **Seguridad perimetral y CSP estricta**: cero atributos `style` inline y cero scripts inline en HTML, Svelte, JS o SVG; lecturas locales siempre con `O_NOFOLLOW`/`O_NONBLOCK` sin filtrar rutas locales en la API.
6. **Accesibilidad**: mantén contraste WCAG AA en los 4 temas (oscuro y claro), `tabular-nums`, `font-kerning: none`, navegación por teclado y soporte de `prefers-reduced-motion`.

## Comprobaciones antes de abrir un Pull Request

Ejecuta los mismos controles que ejecuta CI (`.github/workflows/ci.yml`):

```sh
# 1. Formato y análisis estático Go
test -z "$(gofmt -l cmd internal web)"
CGO_ENABLED=0 go vet ./...

# 2. Frontend Svelte 5 (tipado, unitarias y compilación embebida)
(cd web && npm run check && npm test && npm run build)

# 3. Pruebas unitarias e integración Go (cero CGo)
CGO_ENABLED=0 go test -count=1 -v ./...

# 4. Compilación del binario único
CGO_ENABLED=0 go build -o bin/ai-usage-monitor ./cmd/server
```

Si modificas la interfaz, rutas HTTP, autenticación, SSE o CSP, ejecuta también la auditoría E2E en navegador real:

```sh
(cd web && npx playwright install chromium && npm run test:e2e)
```

## Estilo de commits y Pull Requests

- Mantén los cambios acotados y acompañados de pruebas unitarias o de integración.
- Actualiza la documentación (`README.md`, `docs/`, `SECURITY.md`) si cambias variables de configuración, endpoints o comportamientos observables.
- Completa la lista de verificación de la plantilla de Pull Request.

## Licencia de las contribuciones

Al enviar una contribución a este repositorio, aceptas que tu aportación se distribuya bajo la misma licencia **MIT** del proyecto ([LICENSE](LICENSE)).
