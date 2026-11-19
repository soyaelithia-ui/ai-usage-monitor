# Seguridad

El panel trata las métricas y los identificadores de sesiones como información privada. El repositorio contiene código y fixtures sintéticos; credenciales, bases de datos, cachés locales, capturas y registros de producción permanecen fuera del control de versiones.

## Controles

- **Sellado ligado a máquina**: token aleatorio de 256 bits sellado con HMAC-SHA256 usando `machine.key` (`0600`, junto a la base SQLite). Sin esa clave no es posible validar ni forzar tokens o sesiones fuera de línea; borrar `machine.key` y reiniciar el servicio revoca todos los accesos vigentes.
- **Sesión y CSRF**: token de un solo uso con canje atómico, caducidad de 8 horas desde la emisión, cookie `__Host-usage-monitor` (`Secure`, `HttpOnly`, `SameSite=Strict`) y token CSRF derivado mediante el mismo sellado de máquina.
- **Revocación SSE inmediata**: al cerrar sesión, caducar o ejecutar `ai-usage-monitor revoke`, el servidor emite `event: expired` y cierra las conexiones SSE activas mientras el cliente limpia el estado y redirige a `/access`.
- **Perímetro HTTP (`internal/security`)**:
  - Rechazo de secuencias de traversal (`..`, `%2e%2e`, `%252e`, `\`, `%5c`, `\0`, `%00`), colapso de barras iniciales múltiples y respuesta `404` instantánea ante rutas de escaneo (`/.env`, `/.git`, `/wp-login.php`, `/phpmyadmin`, etc.).
  - Rechazo `403` de cabeceras de suplantación o encadenamiento de proxy (`X-Forwarded-*`, `X-Real-IP`, `Forwarded`, `Forwarded-*`, `Via`) y de cabeceras sensibles duplicadas (`Host`, `Origin`, `Content-Type`).
  - Rechazo `403` de user-agents de escáneres/bots automatizados en rutas de navegador y límite global de 600 solicitudes/minuto.
  - Política estricta de `Origin` (origen configurado, alias loopback en desarrollo y comodines de un solo label `https://*.dominio`), `Content-Type: application/json` obligatorio en mutaciones y bloqueo de framing (`Sec-Fetch-Dest: iframe|frame|embed|object`, `X-Frame-Options: DENY`, `frame-ancestors 'none'`).
  - Cabecera CSP estricta (`default-src 'none'; script-src 'self'; style-src 'self'; ...`) con **cero estilos inline** en HTML, Svelte, JS y SVGs.
- **Ingesta local aislada**: `POST /api/ingest` y `POST /api/agents/register` exigen credencial independiente `Authorization: Bearer <INGEST_KEY>`, rechazan peticiones con `Origin` o `X-Usage-Monitor-Gateway`, sanitizan `iconUrl` (prohibiendo data URIs SVG y traversals) y bloquean claves de rutas locales en registro HTTP.
- **Lectura pasiva segura del sistema de archivos**: `O_RDONLY | O_NONBLOCK | O_NOFOLLOW`, comprobación previa con `os.Lstat`, rechazo de symlinks y FIFOs, límite de profundidad (`MAX_DEPTH = 5`) y apertura de SQLite externo con `mode=ro` y `query_only=1` sin crear `-shm`.
- **Resolución opcional de tarifas externas**: desactivada por defecto (`pricingSyncEnabled: false`). Si se habilita, solo permite HTTPS (sin redirecciones a HTTP), aplica timeout de 5 s, límite de 20 MiB, caché local `0600` y aritmética decimal exacta con validación fail-closed.
- **Binario único sin CGo**: elimina superficies de C/Python en tiempo de ejecución y facilita el aislamiento con `ops/systemd/ai-usage-monitor.service`.

## Informar de una vulnerabilidad

Utiliza el canal privado de avisos de seguridad del repositorio cuando esté disponible. No publiques credenciales, conversaciones, bases de datos ni identificadores privados en incidencias públicas; incluye una reproducción mínima con datos sintéticos.
