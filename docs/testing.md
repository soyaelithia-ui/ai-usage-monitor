# Pruebas y validación

```sh
npm ci
npm test
npm run build
npx playwright install chromium
npm run test:browser
```

Las pruebas unitarias y de integración usan bases de datos temporales y fixtures sintéticos. Cubren enteros grandes, costes decimales, caché y razonamiento, deduplicación, conflictos, rotación y escrituras parciales, reinicios, filtros y agregaciones independientes de la paginación.

Las pruebas de navegador revisan escritorio y móvil, navegación por agente, estados vacíos, actualización SSE, reconexión, historial, autenticación y ausencia de credenciales en almacenamiento web. `CHROMIUM_PATH` permite seleccionar un navegador instalado. En entornos de pruebas que no permiten el sandbox del navegador puede establecerse `CHROMIUM_NO_SANDBOX=1`; esta opción no modifica los servidores de producción.

Para probar el worker después de instalarlo:

```sh
USAGE_MONITOR_TOKENIZER_CACHE="$HOME/.local/share/ai-usage-monitor/tokenizer-cache" \
TIKTOKEN_CACHE_DIR="$HOME/.local/share/ai-usage-monitor/tokenizer-cache/tiktoken" \
  "$HOME/.local/share/ai-usage-monitor/tokenizer-venv/bin/python" worker/test_tokenizer.py
```

La comprobación opcional de una instalación HTTPS utiliza un acceso temporal independiente y elimina únicamente sus registros:

```sh
node ops/verify-deployment.mjs
```

Las capturas y los informes se guardan en `test-results/`, excluido de Git. Nunca utilices métricas de producción como fixtures públicos ni cargues datos de demostración en una base de producción.
