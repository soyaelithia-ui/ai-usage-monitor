# AI Usage Monitor

Panel privado para observar el consumo de agentes de IA en tiempo real. Incluye métricas por respuesta, gráficas por agente y modelo, desglose de caché y razonamiento y una estimación del coste equivalente de API.

React, Express y SQLite. La actualización utiliza Server-Sent Events (SSE). El panel, las API y el flujo de eventos requieren autenticación.

## Funciones

- Apartados independientes por agente y una vista general comparativa.
- Ventanas de 15 minutos, 1 hora, 24 horas y 7 días.
- Filtros compartidos por agente, modelo, proveedor, sesión y método de medición.
- Totales sobre todo el historial seleccionado, independientes de la paginación.
- Contadores enteros exactos y costes calculados con aritmética decimal.
- Deduplicación de respuestas, detección de conflictos y recuperación de cursores.
- Acceso mediante token de un solo uso y sesión con vencimiento de ocho horas desde su emisión.
- Lectores pasivos y tokenización local opcional, sin modificar los agentes.

## Requisitos

Node.js 22.13 o posterior y npm. Python 3.11 o posterior es necesario únicamente para el tokenizador local opcional.

## Inicio rápido

```sh
npm ci
npm run build
npm run setup
```

La configuración se crea fuera del repositorio, con credenciales aleatorias independientes para el gateway y la ingesta. Los directorios predeterminados son `$XDG_CONFIG_HOME/ai-usage-monitor` y `$XDG_DATA_HOME/ai-usage-monitor`, con los directorios estándar del usuario como alternativa. El comando muestra las rutas creadas, sin imprimir las credenciales de servicio. Si la configuración ya existe, se conserva.

Inicia el backend:

```sh
npm start
```

En otra terminal, inicia el gateway con su archivo de configuración:

```sh
USAGE_MONITOR_CONFIG="$HOME/.config/ai-usage-monitor/gateway.json" npm run gateway
```

Si utilizas `XDG_CONFIG_HOME` o un directorio personalizado, sustituye esa ruta por la que mostró el comando de configuración. Abre `http://localhost:4318`. La dirección HTTP de loopback está destinada al desarrollo; los despliegues remotos requieren HTTPS.

Emite un acceso y pega el token en la pantalla de entrada:

```sh
npm run access -- issue
```

Este comando revoca los accesos y sesiones anteriores. El token puede canjearse una sola vez. Tanto el token pendiente como su sesión caducan ocho horas después de la emisión. Para revocar todos los accesos:

```sh
npm run access -- revoke
```

## Configuración

`USAGE_MONITOR_CONFIG` permite seleccionar un archivo de configuración. Para crear otra instalación:

```sh
npm run setup -- --origin https://usage.example.com \
  --config-dir /etc/ai-usage-monitor \
  --data-dir /var/lib/ai-usage-monitor
```

El usuario que ejecute ese comando necesita permisos sobre los directorios elegidos. Configura los lectores mediante estas propiedades opcionales de `backend.json`:

| Propiedad         | Uso                                                                                     |
| ----------------- | --------------------------------------------------------------------------------------- |
| `codexRoot`       | Directorio de registros JSONL con consumo por respuesta.                                |
| `openCodeDb`      | Base SQLite de sesiones de OpenCode 2.1.4.                                              |
| `geminiRoot`      | Directorio de sesiones JSON o JSONL de Gemini CLI.                                      |
| `antigravityRoot` | Directorio de conversaciones para comprobar la disponibilidad de registros compatibles. |

Las rutas se configuran explícitamente. Un lector sin ruta permanece a la espera, sin importar historial ni generar eventos ficticios. Concede acceso de solo lectura a las fuentes y reinicia el backend después de cambiar la configuración. La fecha de activación se conserva en la base de datos; el recolector descarta consumo anterior a ese corte.

## Precisión

La referencia principal son los contadores nativos de cada llamada. Entrada incluye caché y salida incluye razonamiento; los desgloses no vuelven a sumarse. Un valor ausente permanece desconocido. Los formatos que pueden sustituir ausentes por cero se identifican mediante su cobertura.

Cada respuesta conserva su origen, modelo, identificador y momento del consumo. Los reintentos idénticos no duplican los totales; una discrepancia conserva el evento aceptado y registra un conflicto visible.

El contexto puede procesarse repetidamente en distintas llamadas. Las cantidades mostradas representan ese procesamiento, no el tamaño único de la conversación. El importe es un **coste equivalente estimado de API**. No consulta facturas ni cuotas de suscripción. Los modelos o componentes sin tarifa verificable permanecen sin estimar; el panel distingue esos casos de un coste cero.

Consulta [medición y formatos](docs/measurement.md) para conocer los contratos y sus límites.

## Tokenización local opcional

```sh
bash ops/setup-tokenizers.sh
npm run tokenizer
```

Si elegiste otro directorio de datos, establece `USAGE_MONITOR_DATA_DIR` al instalar los tokenizadores. El proceso utiliza el mismo archivo de configuración que el backend.

Se emplean versiones fijadas de tiktoken y Google Gen AI SDK. Los vocabularios se descargan y verifican durante la instalación. La ejecución local utiliza únicamente vocabularios disponibles y correspondencias oficiales; un modelo desconocido se declara sin soporte. Las estimaciones de texto visible se muestran por separado y no se añaden a los totales nativos ni al coste completo de una llamada.

## API

| Endpoint                  | Función                                                                           |
| ------------------------- | --------------------------------------------------------------------------------- |
| `GET /api/analytics`      | Totales, intervalos y distribución de la selección completa.                      |
| `GET /api/events`         | Historial paginado con los mismos filtros.                                        |
| `GET /api/sources`        | Estado de lectores y capacidades de tokenización.                                 |
| `GET /api/reconciliation` | Concordancias, conflictos y diagnósticos parciales.                               |
| `GET /api/stream`         | Notificaciones SSE para reconstruir las consultas.                                |
| `GET /auth/session`       | Estado y vencimiento de la sesión.                                                |
| `POST /auth/exchange`     | Canje atómico de un token de acceso.                                              |
| `POST /auth/logout`       | Cierre de sesión protegido mediante CSRF.                                         |
| `POST /api/ingest`        | Ingesta local con credencial de servicio independiente; bloqueada por el gateway. |

Las consultas aceptan `window`, `agent`, `model`, `provider`, `sessionId`, `method` y `to`. El historial acepta además `limit` y `offset`. Los enteros y costes se transportan como cadenas decimales para conservar precisión. Los contratos están en [src/types.ts](src/types.ts).

## Despliegue y comprobaciones

- [Despliegue con HTTPS y systemd](docs/deployment.md).
- [Pruebas y validación](docs/testing.md).
- [Modelo de seguridad](SECURITY.md).
- [Dependencias e integraciones](NOTICE.md).

Los nombres de herramientas y proveedores se usan únicamente para describir compatibilidad. El proyecto no incluye sus logotipos ni afirma afiliación con sus titulares.
