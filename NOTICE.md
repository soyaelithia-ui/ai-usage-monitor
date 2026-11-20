# Dependencias, licencias y avisos

**AI Usage Monitor** se distribuye bajo la licencia **MIT** (consulta [LICENSE](LICENSE)).

## Marcas e integraciones

Los nombres de agentes, modelos, bibliotecas y proveedores aparecen únicamente para identificar formatos, dependencias y compatibilidad técnica. No se afirma patrocinio, afiliación ni certificación con sus titulares.

## Dependencias de terceros

El proyecto se compila como un binario único Go (`CGO_ENABLED=0`) con activos frontend en Svelte 5 embebidos mediante `go:embed`. Las dependencias declaradas en [go.mod](go.mod) y [web/package.json](web/package.json) conservan sus respectivas licencias y avisos de copyright.

## Fuente tipográfica embebida

- `web/public/fonts/inter-latin.woff2` es un subconjunto latino de **Inter** (Rasmus Andersson), distribuido bajo la licencia **SIL Open Font License 1.1** y autoalojado porque la CSP del servidor restringe `font-src` a `'self'`. El texto completo de la licencia acompaña a la distribución original en <https://github.com/rsms/inter>.
