# Despliegue

El backend y el gateway escuchan exclusivamente en loopback. Publica el gateway mediante un proxy HTTPS y mantén el puerto de ingesta fuera del acceso externo.

## Archivos y cuentas de servicio

Las plantillas de [ops/systemd](../ops/systemd) utilizan estas ubicaciones convencionales, que pueden adaptarse:

| Ubicación                            | Contenido                                                                                            |
| ------------------------------------ | ---------------------------------------------------------------------------------------------------- |
| `/opt/ai-usage-monitor`              | Código y compilación, legibles por las cuentas de servicio y modificables solo por el administrador. |
| `/etc/ai-usage-monitor/backend.json` | Configuración privada del backend.                                                                   |
| `/etc/ai-usage-monitor/gateway.json` | Configuración del gateway, sin credencial de ingesta.                                                |
| `/var/lib/ai-usage-monitor`          | Base de datos, socket y recursos del tokenizador.                                                    |
| `/srv/agent-logs`                    | Fuentes opcionales con acceso de solo lectura.                                                       |

Crea las cuentas `usage-monitor` y `usage-monitor-gateway`. Ejecuta el comando de configuración con permisos sobre los directorios elegidos:

```sh
npm run setup -- --origin https://usage.example.com \
  --config-dir /etc/ai-usage-monitor \
  --data-dir /var/lib/ai-usage-monitor
```

El directorio de configuración debe permitir el recorrido a las cuentas de servicio. Los archivos deben ser propiedad del administrador, con permisos `0640` y grupos `usage-monitor` y `usage-monitor-gateway`, respectivamente. El directorio de datos debe pertenecer a `usage-monitor`, con permisos `0700`; SQLite y el socket utilizan `0600`.

Instala las dependencias y ejecuta `npm run build` antes de iniciar servicios. Si habilitas tokenización local, instala sus recursos como `usage-monitor`, definiendo `USAGE_MONITOR_DATA_DIR=/var/lib/ai-usage-monitor`. Las plantillas presuponen Node.js en `/usr/bin/node`; ajusta `ExecStart` si la instalación utiliza otra ubicación.

Los lectores permanecen desactivados hasta configurar sus rutas. Concede acceso exclusivamente a los registros necesarios. Para directorios de usuario protegidos, añade bind mounts de solo lectura a las rutas neutrales del servicio; no abras el directorio personal completo ni copies credenciales.

Después de revisar las plantillas:

```sh
sudo install -m 0644 ops/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now usage-monitor-backend usage-monitor-gateway
```

El worker es opcional. Actívalo después de instalar sus dependencias:

```sh
sudo systemctl enable --now usage-monitor-tokenizer
```

## Proxy HTTPS

Configura el origen exacto en ambos archivos JSON. En un bloque HTTPS de Nginx ya configurado, el reenvío al gateway puede utilizar:

```nginx
location / {
    proxy_pass http://127.0.0.1:4318;
    proxy_http_version 1.1;
    proxy_set_header Host 127.0.0.1:4318;
    proxy_set_header X-Forwarded-Host usage.example.com;
    proxy_set_header X-Forwarded-Proto https;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

El proxy debe sobrescribir esas cabeceras con valores de confianza. No publiques el backend ni el servidor de desarrollo. El gateway exige sesión para leer métricas y bloquea `/api/ingest`.

## Operación

Emite y revoca accesos desde una sesión administrativa con acceso al archivo de configuración del backend. `npm run access -- issue` revoca los accesos anteriores; no lo utilices como comprobación rutinaria.

Usa la API de backup de SQLite para copias consistentes. Evita copiar únicamente el archivo principal mientras WAL está activo. Conserva copias, capturas, configuración y registros fuera del repositorio público.

`node ops/verify-deployment.mjs` comprueba HTTPS con una sesión temporal independiente. Para inspeccionar el estado de los servicios:

```sh
sudo systemctl status usage-monitor-backend usage-monitor-gateway usage-monitor-tokenizer
```
