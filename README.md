# Pulse · Monitor local de agentes

Dashboard en español para recibir métricas de agentes, calcular gasto estimado en USD y ver actualizaciones en vivo. React + TypeScript + Vite, Express, SSE y SQLite. No necesita claves de proveedores ni realiza llamadas a modelos.

## Arranque

Requiere Node.js 22.13 o posterior y npm. Ejecuta desde este directorio:

```sh
npm ci
npm run build
npm start
```

El repositorio incluye el código y `package-lock.json`. Las dependencias se descargan desde npm con `npm ci`; no requiere archivos ni paquetes de otra carpeta del equipo original. La base de datos y los tokens se crean en el primer arranque correspondiente y no forman parte del repositorio.

Abre **http://127.0.0.1:3000**. `npm start` sirve API y dashboard desde un solo proceso. Para desarrollo: `npm run dev`; interfaz en **http://127.0.0.1:5173** y API en el puerto 3000. Vite reenvía `/api` al backend.

En este sandbox, localhost corresponde a la VM. Puedes abrir la dirección en su navegador desde la pestaña **Computer**. Para verla en tu equipo, usa un túnel SSH al puerto 3000 de la VM. No es necesario publicar ningún puerto.

## Acceso público con Instabox

Esta opción hace que **cualquiera que tenga el enlace pueda leer los nombres de agentes, modelos, consumo, costos e historial**. La ingesta por Internet requiere un token. El servicio local del puerto 3000 sigue aceptando ingesta local sin token.

Con el backend compilado y `npm start` en marcha, inicia en otra terminal:

```sh
npm run start:public-gateway
instabox-port publish 3001 --name pulse
```

Abre el enlace HTTPS que devuelve Instabox. El gateway escucha solo en `127.0.0.1:3001` y reenvía los datos al backend del puerto 3000, sin abrir directamente el puerto 3000 a Internet. En el primer arranque crea `data/ingest-token` con permisos `0600`; conserva ese archivo para que los agentes puedan seguir usando el mismo token tras reiniciar. No publiques ni compartas el token con lectores del dashboard. Para detener la publicación: `instabox-port unpublish 3001`.

Para enviar desde otro equipo, copia el token del archivo a un almacén de secretos del agente y usa HTTPS:

```sh
# TOKEN_DE_INGESTA debe obtenerse de data/ingest-token en el sandbox.
curl "$PULSE_URL/api/usage" \
  -H "Authorization: Bearer $TOKEN_DE_INGESTA" \
  -H 'Content-Type: application/json' \
  -d '{"agent_name":"remoto","model":"gpt-6.1-sol","prompt_tokens":1000,"completion_tokens":500,"request_id":"remoto-001"}'
```

`PULSE_URL` es el enlace HTTPS publicado por Instabox, sin barra final. El dashboard incluye un ejemplo preparado para la dirección desde la que se abre. El gateway no habilita CORS; las páginas externas no pueden leer datos mediante JavaScript de otro origen. Una respuesta `401` de la ingesta indica que falta el token o es incorrecto.

Ambos procesos deben estar activos: el enlace funciona mientras el sandbox y los servicios estén en marcha. Al detener el sandbox terminan los procesos; vuelve a iniciar `npm start` y `npm run start:public-gateway` para recuperar el servicio. La publicación de puertos se puede consultar con `instabox-port list`.

Configuración mediante variables de entorno:

| Variable | Valor predeterminado | Uso |
|---|---|---|
| `PORT` | `3000` | Puerto de la versión compilada/API; el proxy de desarrollo usa 3000 |
| `DB_PATH` | `data/usage.sqlite` | Archivo SQLite persistente |
| `PRICING_PATH` | `config/pricing.json` | Catálogo de tarifas |
| `PUBLIC_GATEWAY_PORT` | `3001` | Puerto local del gateway público |
| `INGEST_TOKEN_PATH` | `data/ingest-token` | Archivo privado del token de ingesta remota |

Las rutas relativas se resuelven desde el directorio de trabajo. El servidor escucha exclusivamente en `127.0.0.1`. Usa un solo proceso por archivo de datos: los acumulados y las suscripciones SSE se mantienen en memoria y se reconstruyen desde SQLite al arrancar. No se inicia automáticamente al reiniciar la máquina.

## Conectar un agente

Envía un evento **al finalizar cada petición al proveedor**, con los contadores de uso que devuelve esa petición; no envíes los acumulados de toda la conversación. En respuestas transmitidas por streaming, espera al uso final.

```sh
curl http://127.0.0.1:3000/api/usage \
  -H 'Content-Type: application/json' \
  -d '{"agent_name":"investigador","model":"gpt-6.1-sol","prompt_tokens":1000,"completion_tokens":500,"request_id":"peticion-001"}'
```

Ese ejemplo cuesta **$0.007** con la tarifa inicial de Sol. Genera un `request_id` distinto por petición real y conserva el mismo en sus reintentos de telemetría. Es opcional; sin él, cada POST cuenta como un evento independiente. El identificador es único en toda la base de datos, no por agente. Repetirlo con datos diferentes devuelve `409`.

Ejemplo TypeScript, reutilizable con el uso devuelto por cualquier SDK:

```ts
import { randomUUID } from 'node:crypto';

const metric = {
  agent_name: 'investigador',
  model: 'gpt-6.1-sol',
  prompt_tokens: 1000,       // Sustituir por el input de esta llamada.
  completion_tokens: 500,   // Sustituir por el output facturable de esta llamada.
  request_id: randomUUID(), // Crear una vez; reutilizar en reintentos.
};

async function reportUsage() {
  const response = await fetch('http://127.0.0.1:3000/api/usage', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(metric),
    signal: AbortSignal.timeout(5000),
  });
  if (!response.ok) throw new Error(`Telemetría: HTTP ${response.status}`);
  return response.json();
}
```

No envíes prompts, respuestas ni claves API. Se almacenan únicamente nombres, modelo, contadores, identificador, hora UTC y precios/costos. Los nombres deben tener entre 1 y 160 caracteres; `request_id`, hasta 200. Los tokens deben ser enteros no negativos representables exactamente en JSON/JavaScript. El cuerpo máximo es 16 KiB.

### API

| Endpoint | Resultado |
|---|---|
| `POST /api/usage` | `201 { event, duplicate: false }`; reintento idéntico: `200 { event, duplicate: true }` |
| `GET /api/stream` | SSE: `snapshot` inicial y `usage` por cada nueva ingesta |
| `GET /api/events?before_id=101&limit=100` | `{ events, next_before_id }`, orden más reciente primero; `limit` entre 1 y 100 |
| `GET /api/pricing` | Catálogo cargado al arrancar, con `server_date` UTC |
| `GET /api/health` | `{ "status": "ok", "storage": "sqlite" }` |

Los costos se devuelven como **strings decimales exactos**, o `null` cuando falta tarifa. Los tokens agregados también son strings para no perder precisión al superar el límite entero de JavaScript. Los tokens de cada evento son números. `400` indica datos inválidos; `409`, conflicto de idempotencia; `413`, cuerpo demasiado grande; `415`, tipo de contenido incorrecto.

SSE envía una instantánea consistente con acumulados globales, grupos por agente/modelo y los últimos 100 eventos. En cada reconexión reemplaza el estado completo; no reproduce deltas a partir de `Last-Event-ID`. Luego transmite eventos posteriores al commit en SQLite. Heartbeat cada 15 segundos y reintento cada 1,5 segundos. El historial anterior se consulta por cursor, sin interferir con la recepción en vivo.

## Cambiar precios o añadir modelos

Edita `config/pricing.json` y reinicia. Los identificadores de modelo se comparan exactamente, sin adivinar alias ni versiones. Ejemplo de entrada:

```json
{
  "provider": "Mi proveedor",
  "source": "https://example.com/precios",
  "rates": [
    {
      "effective_from": "2026-10-28",
      "input_per_million": "2",
      "output_per_million": "10",
      "long_context": {
        "above_prompt_tokens": 272000,
        "input_per_million": "4",
        "output_per_million": "15"
      }
    }
  ]
}
```

Añade esta entrada bajo `models` con el identificador del modelo como clave. `long_context` es opcional. Las fechas de `rates` deben estar ordenadas, ser válidas y no repetirse; los precios son strings decimales no negativos. Una configuración inválida impide el arranque para no calcular importes con datos incorrectos.

La tarifa se selecciona por **fecha UTC de recepción**, con el último `effective_from` aplicable. No se importan eventos retroactivos. Los precios con fecha futura se activan automáticamente al llegar esa fecha, sin reiniciar. Para contexto largo, el tramo se activa estrictamente por encima del límite y se aplica a toda la petición.

```text
costo = (prompt_tokens × input_per_million
       + completion_tokens × output_per_million) / 1.000.000
```

El catálogo inicial, verificado el 28/10/2026, incluye Sol, Luna, Opus 5.5, Sonnet 5.5, Gemini 3.8 Flash y Gemini 3.5 Flash-Lite. Incluye el cambio de Gemini 3.8 Flash anunciado para el 01/01/2027. Cada modelo registra su URL oficial. No hay actualización automática desde Internet.

El gasto es una **estimación de texto a tarifa estándar de pago**, no una conciliación de facturas: excluye caché, Batch, herramientas, impuestos, cuotas gratuitas y descuentos particulares. Incluye en `completion_tokens` el total facturable de salida —también razonamiento si corresponde—, sin contar dos veces tokens que el proveedor ya incluya.

Los eventos de modelos desconocidos o sin tarifa vigente se guardan como **Sin tarifa**: sus tokens sí suman, su costo no. El dashboard muestra cuántos eventos se excluyen del gasto. Actualizar precios o añadir modelos solo afecta eventos nuevos; los costos históricos y los eventos sin tarifa anteriores no se recalculan.

## Pruebas y demostración

```sh
npm test
npm run build
npx playwright install chromium
npm run test:browser
```

Las pruebas del navegador usan el Chromium instalado por Playwright, una base de datos temporal y un puerto dinámico. También puedes usar un navegador ya instalado definiendo `CHROMIUM_PATH` con su ruta. En Linux pueden ser necesarias las bibliotecas del sistema que instala `npx playwright install --with-deps chromium`. Comprueban dos clientes en vivo, reconexión tras reinicio, persistencia, modelo desconocido, copiar cURL, tarifas, paginación y diseño móvil. Las capturas quedan en `test-results/`. No escriben en la base de datos real.

El hook opcional `scripts/antigravity-hook.py` usa únicamente la biblioteca estándar de Python 3. Localiza las transcripciones en el directorio del usuario actual y conserva su caché bajo `data/` de este proyecto. Puedes configurar esas rutas con `ANTIGRAVITY_BRAIN_DIR` y `PULSE_DATA_DIR`.

`npm run demo` envía 30 eventos de ejemplo al servicio activo; **se guardan y suman al gasto** bajo agentes `demo/…`. Puedes aislarlos arrancando una segunda instancia:

```sh
DB_PATH=data/demo.sqlite PORT=3001 npm start
# En otra terminal:
API_URL=http://127.0.0.1:3001 npm run demo
```

Abre http://127.0.0.1:3001 para esa demostración. No se generan datos ficticios automáticamente.

## Datos y operación local

SQLite usa WAL y conserva todos los eventos. Para una copia sencilla, detén la aplicación y copia el archivo configurado en `DB_PATH`; evita copiar solo el archivo principal mientras el servicio escribe. Los archivos de datos, compilados, dependencias y capturas están excluidos de Git.

La interfaz muestra hasta 100 eventos por página y mantiene los acumulados de **todo** el historial. El backend imprime fallos operativos en la terminal y el dashboard señala pérdidas de conexión conservando el último estado. No hay autenticación multiusuario para consultar el dashboard; si se publica con Instabox, sus datos son públicos para cualquier persona que tenga el enlace.
