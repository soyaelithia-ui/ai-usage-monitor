# Dependencias e integraciones

Los nombres de agentes, modelos, bibliotecas y proveedores aparecen únicamente para identificar formatos, dependencias y compatibilidad técnica. No se afirma patrocinio, afiliación ni certificación con sus titulares.

El proyecto se compila como un binario único Go (`CGO_ENABLED=0`) con activos frontend en Svelte 5 embebidos mediante `go:embed`. Las dependencias declaradas en `go.mod` y `web/package.json` conservan sus respectivas licencias y avisos.

Excepción de fuente tipográfica: `web/public/fonts/inter-latin.woff2` es un subconjunto latino de Inter (Rasmus Andersson), licencia SIL Open Font License 1.1, autoalojado porque la CSP del servidor restringe `font-src` a `'self'`. El texto completo de la licencia acompaña a la distribución original en <https://github.com/rsms/inter>.

La visibilidad pública de este repositorio no concede automáticamente una licencia de reutilización sobre el código del proyecto hasta que su titular publique una licencia explícita.
