# Despliegue

El binario único **AI Usage Monitor** (`cmd/server`) escucha exclusivamente en loopback (`127.0.0.1`). Publica el servicio tras un proxy HTTPS de borde y bloquea `/api/ingest` y `/api/agents/register` desde Internet.

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

Ejemplo mínimo de `/etc/ai-usage-monitor/backend.json`:

```json
{
  "origin": "https://usage.example.com",
  "allowedOrigins": ["https://panel.example.com"],
  "backendPort": 8080,
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

## Proxy HTTPS

El backend rechaza de forma estricta cabeceras de reenvío forjadas (`X-Forwarded-*`, `X-Real-IP`, `Forwarded`, `Via`). Configura el proxy HTTPS para eliminar esas cabeceras antes de reenviar hacia loopback y bloquear las rutas de ingesta local desde el exterior:

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

## Operación y copias de seguridad

Emitir o revocar accesos desde una sesión administrativa:

```sh
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json /usr/local/bin/ai-usage-monitor issue
sudo -u usage-monitor USAGE_MONITOR_CONFIG=/etc/ai-usage-monitor/backend.json /usr/local/bin/ai-usage-monitor revoke
```

- `issue` revoca cualquier acceso o sesión anterior y genera un token de un solo uso válido durante 8 horas.
- Borrar `/var/lib/ai-usage-monitor/machine.key` y reiniciar el servicio invalida de inmediato todas las sesiones y tokens sellados.
- Usa `sqlite3 /var/lib/ai-usage-monitor/usage.db ".backup '/ruta/backup.db'"` para copias consistentes con WAL activo.
- Comprueba el estado del servicio con `sudo systemctl status ai-usage-monitor`.
