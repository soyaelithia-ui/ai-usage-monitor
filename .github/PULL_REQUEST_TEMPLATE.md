## Resumen

Describe brevemente qué cambia este Pull Request y por qué.

## Incidencia relacionada

- Cierra #

## Lista de verificación

- [ ] `gofmt -l cmd internal web` no reporta cambios pendientes
- [ ] `CGO_ENABLED=0 go vet ./...` pasa sin errores
- [ ] `CGO_ENABLED=0 go test -count=1 ./...` pasa completamente
- [ ] Frontend verificado (`cd web && npm run check && npm test && npm run build`) si aplica
- [ ] Auditoría E2E (`cd web && npm run test:e2e`) ejecutada si afecta a UI, rutas HTTP, auth, SSE o CSP
- [ ] No se incluyen secretos, claves, bases de datos, rutas locales ni datos reales de conversaciones
- [ ] Documentación actualizada si cambian variables de entorno, endpoints o comportamientos
