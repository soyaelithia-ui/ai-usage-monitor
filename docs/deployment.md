# Despliegue

El binario único **AI Usage Monitor** (`cmd/server`) escucha por defecto en loopback (`127.0.0.1:8080`) con autenticación por token de un solo uso. Para exposición en Internet, publica el servicio tras un proxy HTTPS de borde y bloquea `/api/ingest` y `/api/agents/register` desde el exterior. Para uso doméstico o de oficina, también admite despliegue directo en red local (`--lan`, `bindAddr`, `allowLan`), tokens permanentes o estáticos (`--permanent`, `accessToken`) y un modo opcional sin token restringido estrictamente a direcciones IP de red local (`AUTH_MODE=local` / `--local`).

## Compilación del binario único

```sh
cd web
npm ci
npm run build
cd ..
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/ai-usage-monitor ./cmd/server
sudo install -m 0755 bin/ai-usage-monitor /usr/local/bin/ai-usage-monitor
```

## Archivos y cuenta de servicio

La unidad [ops/systemd/ai-usage-monitor.service](../ops/systemd/ai-usage-monitor.service) utiliza estas ubicaciones convencionales:

| Ubicación                            | Contenido                                                                           |
| ------------------------------------ | ----------------------------------------------------------------------------------- |
| `/usr/local/bin/ai-usage-monitor`    | Binario Go puro (sin CGo) con los activos Svelte 5 embebidos.                       |
| `/etc/ai-usage-monitor/backend.json` | Configuración privada (`0640`, propietario `root:usage-monitor`).                   |
| `/var/lib/ai-usage-monitor`          | Base SQLite WAL (`usage.db`), `machine.key` (`0600`) y `pricing-cache.json` (`0600`). |
| `/srv/agent-logs`                    | Montajes opcionales de solo lectura hacia registros locales de agentes.             |

Ejemplo mínimo de `/etc/ai-usage-monitor/backend.json` para proxy HTTPS:

```json
{
  "origin": "https://usage.example.com",
  "allowedOrigins": ["https://panel.example.com"],
  "backendPort": 8080,
  "bindAddr": "127.0.0.1",
  "authMode": "token",
  "allowLan": false,
  "db": "/var/lib/ai-usage-monitor/usage.db",
  "ingestKey": "<43-char-base64url-secret>",
  "pricingSyncEnabled": false,
  "pricingCachePath": "/var/lib/ai-usage-monitor/pricing-cache.json",
  "pricingSources": ["models.dev", "litellm", "openrouter"]
}
```

Genera secretos aleatorios de 256 bits en formato base64url (43 caracteres), por ejemplo con `openssl rand 32 | basenc --base64url | tr -d '='`.

Instalación de la unidad systemd:

```sh
sudo install -d -m 0750 -o root -g usage-monitor /etc/ai-usage-monitor
sudo install -d -m 0700 -o usage-monitor -g usage-monitor /var/lib/ai-usage-monitor
sudo install -m 0644 ops/systemd/ai-usage-monitor.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ai-usage-monitor
```

## Proxy HTTPS (producción en Internet)

El backend rechaza de forma estricta cabeceras de reenvío forjadas (`X-Forwarded-*`, `X-Real-IP`, `Forwarded`, `Forwarded-*`, `Via`) con `403 Forbidden`. Configura el proxy HTTPS para eliminar esas cabeceras antes de reenviar hacia loopback y bloquear las rutas de ingesta local desde el exterior:

```nginx
location ~ ^/api/(ingest|agents/register)$ {
    return 404;
}

location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host 127.0.0.1:8080;
    proxy_set_header X-Forwarded-For "";
    proxy_set_header X-Forwarded-Host "";
    proxy_set_header X-Forwarded-Proto "";
    proxy_set_header X-Real-IP "";
    proxy_set_header Forwarded "";
    proxy_set_header Via "";
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

## Despliegue en red local (LAN)

Puedes exponer el panel a otros dispositivos de tu red local de dos formas: directamente desde el binario o tras un proxy inverso en la propia LAN.

### Opción A: LAN directa (HTTP o HTTPS)

Para escuchar en todas las interfaces o en una IP privada concreta de tu red local:

```sh
# Escucha en 0.0.0.0:8080 con orígenes LAN habilitados y modo token
./bin/ai-usage-monitor --lan

# O vincula una IP privada concreta y puerto personalizado
./bin/ai-usage-monitor --bind 192.168.1.50 --port 8080
```

O mediante `/etc/ai-usage-monitor/backend.json`:

```json
{
  "bindAddr": "192.168.1.50",
  "backendPort": 8080,
  "allowLan": true,
  "authMode": "token",
  "db": "/var/lib/ai-usage-monitor/usage.db"
}
```

Comportamiento de orígenes y cookies en LAN directa:

- Si `bindAddr` no es loopback ni dirección no especificada (`0.0.0.0` / `::`) y no defines `origin`, el servidor toma `http://<bindAddr>:<port>` como origen principal por defecto (con `0.0.0.0` mantiene `http://127.0.0.1:<port>`).
- Cualquier `bindAddr` no-loopback o `authMode: "local"` activa automáticamente `allowLan: true`. Con `allowLan: true`, `Origin` permite peticiones HTTP/HTTPS procedentes de subredes privadas/locales (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `fc00::/7`, `fe80::/10`, loopback) y nombres mDNS `*.local`. Los orígenes externos (p. ej. `https://evil.com`) y las peticiones `Sec-Fetch-Site: cross-site` siguen rechazándose con `403`.
- **Cookies en LAN HTTP vs HTTPS**: los navegadores rechazan cookies con prefijo `__Host-` o atributo `Secure` sobre `http://` no-loopback. Por ello, cuando `allowLan` está activo y la petición llega por HTTP plano no-loopback (p. ej. `http://192.168.1.50:8080` o `http://monitor.local:8080`), el servidor emite la cookie `usage-monitor` (`Secure: false`, `HttpOnly`, `SameSite=Strict`). En HTTPS o loopback HTTP (`127.0.0.1`, `localhost`, `::1`) mantiene `__Host-usage-monitor` (`Secure: true`).
- **Nota de transporte**: HTTP plano en LAN no cifra tokens ni cookies en tránsito. Usa HTTPS (opción B) si compartes la red Wi-Fi/LAN con dispositivos no confiables.
- Si utilizas [ops/systemd/ai-usage-monitor.service](../ops/systemd/ai-usage-monitor.service), `RestrictAddressFamilies=AF_INET AF_INET6` ya permite sockets IPv4/IPv6 en LAN; restringe igualmente el puerto `8080` en el firewall (`ufw`/`nftables`) a tu subred local.

### Opción B: LAN tras proxy inverso (HTTPS o dominio `.local`)

Para publicar el panel en LAN con TLS (p. ej. `https://usage.home.local` o `https://192.168.1.50` con Nginx o Caddy):

1. Mantén el backend escuchando en loopback (`"bindAddr": "127.0.0.1"`, `"authMode": "token"`).
2. Configura `"origin": "https://usage.home.local"` (y `"allowedOrigins"` si procede; si terminas sirviendo HTTP plano LAN tras proxy local, activa `"allowLan": true`).
3. Haz que el proxy **limpie todas las cabeceras de reenvío** (`X-Forwarded-*`, `X-Real-IP`, `Forwarded`, `Via`) como en el bloque Nginx anterior; de lo contrario el backend devolverá `403 Forbidden`.
4. Si el proxy envía `Host: usage.home.local` o `Host: 127.0.0.1:8080` por HTTP loopback pero el navegador accede por HTTPS externo, asegúrate de configurar `"origin": "https://usage.home.local"` y que el `Host` reenviado coincida con `origin` (o termina TLS antes manteniendo `proxy_set_header Host usage.home.local`) para que el servidor detecte el esquema HTTPS al emitir la cookie `__Host-usage-monitor`.

> [!WARNING]
> **No combines un proxy inverso con `authMode: "local"`**: el backend comprueba la dirección TCP `RemoteAddr` de la conexión entrante. Detrás de un proxy inverso, `RemoteAddr` será la IP loopback o LAN del proxy, lo que permitiría que clientes externos alcanzaran el panel sin token. Usa siempre `authMode: "token"` detrás de cualquier proxy.

## Tokens permanentes, reutilizables y token estático

Además del token efímero predeterminado (un solo uso, caducidad de 8 horas), existen tres mecanismos para sesiones duraderas o compartidas entre dispositivos:

### Tokens emitidos por CLI (`issue`)

```sh
# Token permanente y reutilizable (no caduca; sesiones de 365d por defecto)
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json \
  /usr/local/bin/ai-usage-monitor issue --permanent

# Token reutilizable con caducidad de 30 días sin revocar sesiones previas
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json \
  /usr/local/bin/ai-usage-monitor issue --reusable --ttl 30d --keep

# Token permanente cuya sesión individual dura 90 días por navegador
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json \
  /usr/local/bin/ai-usage-monitor issue --permanent --ttl 90d
```

- `--permanent`: emite un token reutilizable sin caducidad (`expiresAt: "never"`, `permanent: true`, `reusable: true`) con sesiones de `365d` por defecto (o `--ttl` si se especifica).
- `--reusable`: permite múltiples canjes en `/auth/exchange` hasta que caduque.
- `--ttl <duración>`: admite unidades Go (`h`, `m`, `s`) y sufijos `d` (`24h`) o `w` (`7d`), p. ej. `24h`, `2w`, `30d`, `365d`.
- `--keep`: conserva los tokens de acceso y las sesiones ya activas en SQLite.

### Token estático en configuración (`accessToken` / `ACCESS_TOKEN`)

Para despliegues declarativos (sin ejecutar `issue` tras crear la base de datos), configura un secreto aleatorio de 43 caracteres base64url distinto de `ingestKey` y `gatewayKey`:

```json
{
  "origin": "https://usage.example.com",
  "accessToken": "<43-char-base64url-secret>",
  "sessionTtl": "30d"
}
```

- `accessToken` (`ACCESS_TOKEN`) es reutilizable y permanece válido mientras siga en la configuración; al canjearse en `/auth/exchange` crea una sesión con duración `sessionTtl` (`SESSION_TTL`), o `365d` por defecto si `sessionTtl` está vacío.
- `sessionTtl` (`SESSION_TTL`) también define la duración de sesión por defecto para tokens emitidos sin `--ttl` ni `--permanent`.
- Al rotar o eliminar `accessToken` de la configuración dejan de aceptarse nuevos canjes; para invalidar de inmediato sesiones ya abiertas, ejecuta `revoke` o elimina `machine.key`.

## Modo sin token en red local (`AUTH_MODE=local`)

Para entornos privados donde no quieras introducir un token desde la red local:

```sh
# Solo accesible desde la máquina local (127.0.0.1)
./bin/ai-usage-monitor --local

# Accesible desde toda la red local (0.0.0.0) sin pantalla /access
./bin/ai-usage-monitor --lan --local
```

O mediante configuración/variables de entorno (`AUTH_MODE=local`, `BIND_ADDR=0.0.0.0`; también se normalizan `"lan"`, `"no-token"` y `"local-no-token"` a `"local"`):

```json
{
  "bindAddr": "0.0.0.0",
  "backendPort": 8080,
  "authMode": "local",
  "allowLan": true,
  "db": "/var/lib/ai-usage-monitor/usage.db"
}
```

Qué hace exactamente el modo `local`:

- `GET /auth/session` responde `200` con `{"authMode":"local","tokenRequired":false,"csrf":"","expiresAt":""}` sin requerir cookie ni canje de token.
- El frontend carga directamente `/` sin redirigir a `/access` y oculta el botón **Cerrar sesión**.
- Todo el tráfico pasa por la comprobación `IsLocalNetworkAddr(r.RemoteAddr)` (en el perímetro HTTP, autenticación, SSE y manejador 404): solo acepta loopback (`127.0.0.0/8`, `::1`), direcciones privadas RFC1918 (`10/8`, `172.16/12`, `192.168/16`), link-local unicast (`169.254/16`, `fe80::/10`) y IPv6 ULA (`fc00::/7`). Cualquier IP pública o externa es rechazada con `403 Forbidden`.
- Las rutas de ingesta local (`POST /api/ingest`, `POST /api/agents/register`, `POST /api/hooks/{agent}`) siguen exigiendo `Authorization: Bearer <INGEST_KEY>` y, en modo `local`, también quedan restringidas a `RemoteAddr` de red local.

> [!CAUTION]
> **Advertencias de seguridad del modo sin token (`AUTH_MODE=local`)**:
> - Cualquier usuario o dispositivo conectado a la misma red local (o cuenta local en la máquina) podrá consultar todas las métricas, sesiones y costes sin credenciales.
> - **Nunca** expongas una instancia en modo `local` mediante redirección de puertos del router, túneles públicos (Cloudflare Tunnel, ngrok, Tailscale Funnel) ni proxies inversos externos: el servidor vería la IP local del túnel/proxy y concedería acceso sin token a Internet.
> - Aunque no haya token de sesión, la política de `Origin`, el bloqueo de `Sec-Fetch-Site: cross-site`, el rechazo de cabeceras de proxy y la CSP estricta siguen activos para impedir que sitios web externos en tu navegador lean el panel local.

## Operación y copias de seguridad

Emitir o revocar accesos desde una sesión administrativa:

```sh
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json /usr/local/bin/ai-usage-monitor issue
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json /usr/local/bin/ai-usage-monitor revoke
```

- `issue` (sin flags) revoca cualquier acceso o sesión anterior y genera un token de un solo uso válido durante 8 horas (usa `--permanent`, `--reusable`, `--ttl` o `--keep` según necesidad).
- `revoke` elimina todos los tokens emitidos en SQLite, cierra sesiones activas y revoca de inmediato los flujos SSE (`event: expired`).
- Borrar `/var/lib/ai-usage-monitor/machine.key` y reiniciar el servicio invalida de inmediato todas las sesiones y tokens sellados.
- Usa `sqlite3 /var/lib/ai-usage-monitor/usage.db ".backup '/ruta/backup.db'"` para copias consistentes con WAL activo.
- Comprueba el estado del servicio con `sudo systemctl status ai-usage-monitor`.
