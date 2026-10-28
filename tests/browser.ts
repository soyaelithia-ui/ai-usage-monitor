import assert from 'node:assert/strict';
import { chromium, type Page } from 'playwright';
import { once } from 'node:events';
import type { AddressInfo } from 'node:net';
import { mkdtempSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createApp } from '../server/app.js';
import { UsageStore } from '../server/store.js';
import { loadPricing } from '../server/pricing.js';

process.env.INSTABOX_HEADLESS = '1';
const temp = mkdtempSync(join(tmpdir(), 'pulse-browser-'));
const catalog = loadPricing('config/pricing.json');
const databasePath = join(temp, 'usage.sqlite');
const now = () => new Date('2026-10-28T20:30:00.000Z');
async function serve(port = 0) {
  const store = new UsageStore(databasePath, catalog);
  const service = createApp({ store, catalog, staticDir: resolve('dist/client'), now });
  const server = service.app.listen(port, '127.0.0.1');
  await once(server, 'listening');
  return {
    port: (server.address() as AddressInfo).port,
    stop: async () => {
      service.closeStreams(); server.closeAllConnections();
      await new Promise<void>(resolve => server.close(() => resolve())); store.close();
    },
  };
}

let service = await serve();
const base = `http://127.0.0.1:${service.port}`;
const browser = await chromium.launch({
  ...(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}),
  headless: true,
  args: ['--no-sandbox'],
});
const errors: string[] = [];
let postCounter = 0;
async function send(model = 'gpt-6.1-sol', agent = 'investigador', prompt = 1000, completion = 500) {
  const response = await fetch(`${base}/api/usage`, { method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_name: agent, model, prompt_tokens: prompt, completion_tokens: completion, request_id: `browser-${++postCounter}` }),
  });
  assert.equal(response.status, 201);
}
async function requests(page: Page, count: number) {
  await page.waitForFunction(expected => document.querySelector('[data-testid="request-count"]')?.textContent === String(expected), count, { timeout: 10000 });
}

try {
  const desktop = await browser.newContext({ viewport: { width: 1440, height: 1080 }, permissions: ['clipboard-read', 'clipboard-write'] });
  const secondContext = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await desktop.newPage();
  const second = await secondContext.newPage();
  for (const tab of [page, second]) tab.on('pageerror', error => errors.push(error.message));
  await Promise.all([page.goto(base), second.goto(base)]);
  await Promise.all([requests(page, 0), requests(second, 0)]);
  assert.equal(await page.getByText('A la escucha de tus agentes').count(), 1);
  mkdirSync('test-results', { recursive: true });
  await page.screenshot({ path: 'test-results/empty-dashboard.png', fullPage: true });

  const start = performance.now();
  await send();
  await Promise.all([requests(page, 1), requests(second, 1)]);
  assert.ok(performance.now() - start < 1000, 'Dos navegadores actualizados en menos de un segundo');
  assert.match(await page.getByTestId('total-cost').innerText(), /0,007/);
  await page.getByRole('button', { name: 'Conectar agente', exact: true }).click();
  await page.getByRole('button', { name: 'Copiar cURL' }).click();
  await page.getByRole('button', { name: 'Copiado' }).waitFor();
  assert.match(await page.evaluate(() => navigator.clipboard.readText()), /request_id/);
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('dialog').count(), 0);

  const agents = ['investigador', 'programador', 'revisor', 'coordinador'];
  const models = Object.keys(catalog.models);
  for (let index = 0; index < 17; index++) await send(models[index % models.length], agents[index % agents.length], 12000 + index * 1003, 2800 + index * 211);
  await Promise.all([requests(page, 18), requests(second, 18)]);
  await page.screenshot({ path: 'test-results/dashboard.png', fullPage: true });
  const persistedCost = await page.getByTestId('total-cost').innerText();

  await service.stop();
  await page.getByText('Reconectando', { exact: true }).waitFor();
  assert.equal(await page.getByTestId('total-cost').innerText(), persistedCost);
  service = await serve(service.port);
  await send();
  await Promise.all([requests(page, 19), requests(second, 19)]);
  assert.equal(await page.locator('tbody tr').count(), 19);

  await send('modelo-personalizado', '<img src=x onerror=alert(1)>');
  await requests(page, 20);
  assert.equal(await page.locator('tbody tr').first().getByText('Sin tarifa', { exact: true }).count(), 1);
  assert.equal(await page.locator('tbody img').count(), 0);
  assert.equal(await page.getByRole('alert').count(), 0);
  await page.getByRole('button', { name: 'Tarifas', exact: true }).click();
  await page.getByRole('heading', { name: 'Catálogo de precios' }).waitFor();
  await page.getByText('Desde 2027-01-01:', { exact: false }).waitFor();
  assert.equal(await page.locator('tbody tr').count(), 6);
  await page.screenshot({ path: 'test-results/pricing.png', fullPage: true });
  await page.getByRole('button', { name: 'Vista general', exact: true }).click();

  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, deviceScaleFactor: 1 });
  const phone = await mobile.newPage();
  phone.on('pageerror', error => errors.push(error.message));
  await phone.goto(base); await requests(phone, 20);
  assert.ok(await phone.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'Sin desbordamiento horizontal en móvil');
  await phone.screenshot({ path: 'test-results/mobile.png', fullPage: true });
  await phone.getByRole('button', { name: 'Conectar agente', exact: true }).click();
  await phone.getByRole('button', { name: 'Entendido' }).click();

  for (let index = 20; index < 105; index++) await send();
  await requests(page, 105);
  assert.equal(await page.locator('tbody tr').count(), 100);
  await page.getByRole('button', { name: 'Ver anteriores', exact: true }).click();
  await page.getByRole('button', { name: 'Volver en vivo' }).waitFor();
  assert.equal(await page.locator('tbody tr').count(), 5);
  await send();
  await requests(page, 106);
  assert.equal(await page.locator('tbody tr').count(), 5);
  await page.getByRole('button', { name: 'Volver en vivo' }).click();
  assert.equal(await page.locator('tbody tr').count(), 100);
  assert.deepEqual(errors, []);
  console.log('Browser QA OK: dos clientes <1 s, persistencia/reconexión, copiar, tarifas, modelo desconocido, móvil y paginación.');
} finally {
  await browser.close(); await service.stop(); rmSync(temp, { recursive: true, force: true });
}
