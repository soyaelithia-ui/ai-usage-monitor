import { chromium } from '@playwright/test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, appendFileSync, rmSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
import { openDb, issue, insertEvent, revoke } from '../server/db.js';
import { Collector } from '../server/collector.js';
import { createApp } from '../server/app.js';
import { gateway } from '../server/gateway.js';
const dir = mkdtempSync(join(tmpdir(), 'usage-monitor-browser-')),
  logs = join(dir, 'logs');
mkdirSync(logs);
const db = openDb(join(dir, 'test.sqlite'));
const collector = new Collector(db, logs);
let c = {
  db,
  origin: 'http://localhost:4180',
  gatewayKey: 'test-gateway',
  ingestKey: 'test-ingest',
  collector,
  dist: process.env.USAGE_MONITOR_TEST_DIST,
};
// Fixed test-only ports allow Origin and cookie validation over a trustworthy localhost origin.
c.origin = 'http://localhost:4180';
c.backendPort = 4181;
c.gatewayPort = 4180;
const api = createApp(c);
collector.onChange = api.broadcast;
const backend = api.app.listen(c.backendPort, '127.0.0.1');
const proxy = gateway(c).listen(c.gatewayPort, '127.0.0.1');
await Promise.all([
  new Promise((r) => backend.on('listening', r)),
  new Promise((r) => proxy.on('listening', r)),
]);
collector.start();
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_PATH || undefined,
  headless: true,
  args: ['--no-sandbox', '--disable-setuid-sandbox'],
  chromiumSandbox: process.env.CHROMIUM_NO_SANDBOX === '0',
});
const report = {};
try {
  for (const [name, width, height] of [
    ['desktop', 1440, 1100],
    ['mobile', 390, 844],
  ]) {
    const context = await browser.newContext({
      viewport: { width, height },
      reducedMotion: 'reduce',
    });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('console', (m) => {
      if (m.type() === 'error' && !m.text().includes('401') && !m.text().includes('404'))
        errors.push(m.text());
    });
    await page.route('https://usage-monitor-entry-test.example/', (r) =>
      r.fulfill({
        contentType: 'text/html',
        body: `<a href="${c.origin}">Abrir AI Usage Monitor</a>`,
      }),
    );
    await page.goto('https://usage-monitor-entry-test.example/');
    await page.getByRole('link', { name: 'Abrir AI Usage Monitor' }).click();
    await page.waitForURL('**/access');
    const a = issue(db);
    await page.getByLabel('Token de acceso').fill(a.token);
    await page.getByRole('button', { name: 'Abrir mi panel' }).click();
    await page.waitForURL(c.origin + '/');
    await page.getByRole('heading', { name: 'Cada token, en perspectiva.' }).waitFor();
    if (name === 'desktop') {
      await page.getByRole('heading', { name: 'Esperando la próxima respuesta' }).waitFor();
      const start = Date.now(),
        r = {
          type: 'token_usage_record',
          timestamp: new Date().toISOString(),
          payload: {
            response_id: 'latency-test',
            turn_id: 't',
            session_id: 'native-browser-session',
            usage: {
              input_tokens: 1000,
              cached_input_tokens: 100,
              cache_write_input_tokens: 0,
              output_tokens: 300,
              reasoning_output_tokens: 10,
            },
          },
        };
      writeFileSync(
        join(logs, 'real-format.jsonl'),
        JSON.stringify({ type: 'turn_context', payload: { turn_id: 't', model: 'gpt-6-astra' } }) +
          '\n' +
          JSON.stringify(r) +
          '\n',
      );
      await page
        .getByText('1 respuestas en esta selección', { exact: true })
        .waitFor({ timeout: 5000 });
      report.latencyMs = Date.now() - start;
      assert.ok(report.latencyMs < 5000);
      for (let i = 0; i < 154; i++)
        insertEvent(db, {
          source: 'test',
          provider: 'openai',
          agent: i % 2 ? 'Codex' : 'Antigravity',
          model: i % 2 ? 'gpt-6-astra' : 'gpt-6.1-sol',
          responseId: 'fixture-' + i,
          occurredAt: new Date(Date.now() - i * 500).toISOString(),
          inputTokens: '1000',
          outputTokens: '300',
          cachedInputTokens: '100',
          cacheWriteInputTokens: '0',
          reasoningOutputTokens: '10',
        });
      api.broadcast();
      await page.getByText('155 respuestas en esta selección', { exact: true }).waitFor();
      assert.equal(await page.locator('.agent-card').count(), 2);
      await page
        .getByRole('button', { name: 'Abrir apartado de Antigravity', exact: true })
        .click();
      await page
        .getByRole('heading', { name: 'Antigravity, en tiempo real.', exact: true })
        .waitFor();
      await page.getByText('77 respuestas en esta selección', { exact: true }).waitFor();
      assert.equal(
        await page
          .locator('tbody .agent-badge')
          .evaluateAll((nodes) => nodes.every((n) => n.textContent === 'Antigravity')),
        true,
      );
      const agentStart = Date.now();
      const added = await fetch(`http://127.0.0.1:${c.backendPort}/api/ingest`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', authorization: 'Bearer test-ingest' },
        body: JSON.stringify({
          source: 'test',
          provider: 'openai',
          agent: 'Antigravity',
          model: 'gpt-6.1-sol',
          responseId: 'agent-live',
          occurredAt: new Date().toISOString(),
          inputTokens: '1000',
          outputTokens: '300',
          cachedInputTokens: '100',
          cacheWriteInputTokens: '0',
          reasoningOutputTokens: '10',
        }),
      });
      assert.equal(added.status, 200);
      await page
        .getByText('78 respuestas en esta selección', { exact: true })
        .waitFor({ timeout: 5000 });
      report.agentLatencyMs = Date.now() - agentStart;
      assert.ok(report.agentLatencyMs < 5000);
      await page.getByRole('button', { name: 'Apartado OpenCode', exact: true }).click();
      await page.getByRole('heading', { name: 'OpenCode, en tiempo real.', exact: true }).waitFor();
      await page.getByRole('heading', { name: 'Esperando la próxima respuesta' }).waitFor();
      await page.getByRole('button', { name: 'Apartado Codex', exact: true }).click();
      await page.getByRole('heading', { name: 'Codex, en tiempo real.', exact: true }).waitFor();
      await page.getByText('78 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByRole('button', { name: 'Vista general', exact: true }).click();
      await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByRole('button', { name: 'Siguiente' }).click();
      await page.getByText('51–100', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por agente').selectOption('Antigravity');
      await page.getByText('78 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por agente').selectOption('');
      await page.getByLabel('Filtrar por modelo').selectOption('gpt-6-astra');
      await page.getByText('78 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por modelo').selectOption('');
      await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por proveedor').selectOption('openai');
      await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por sesión').selectOption('native-browser-session');
      await page.getByText('1 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por sesión').selectOption('');
      await page.getByLabel('Filtrar por método').selectOption('native');
      await page.getByText('1 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByLabel('Filtrar por método').selectOption('');
      await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
      await page.getByRole('button', { name: 'Fuentes y precisión', exact: true }).click();
      await page.getByRole('heading', { name: 'Fuentes y precisión' }).waitFor();
      await page.getByRole('button', { name: 'Vista general', exact: true }).click();
      await page.getByRole('heading', { name: 'Qué se está contando' }).waitFor();
      await context.setOffline(true);
      await context.setOffline(false);
      await page.getByText('En vivo', { exact: true }).waitFor({ timeout: 15000 });
      await page.getByLabel('Intervalo de tokens').focus();
      await page.keyboard.press('ArrowLeft');
      await page.getByLabel('Intervalo de coste').focus();
      await page.keyboard.press('ArrowLeft');
    }
    await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      true,
      `${name} page overflow`,
    );
    mkdirSync('test-results', { recursive: true });
    await page.screenshot({ path: `test-results/${name}.png`, fullPage: true });
    // Every agent section remains reachable on mobile and keeps its charts scoped.
    if (name === 'mobile') {
      await page.getByRole('button', { name: 'Mostrar barra lateral', exact: true }).click();
    }
    await page.getByRole('button', { name: 'Apartado Antigravity', exact: true }).click();
    await page
      .getByRole('heading', { name: 'Antigravity, en tiempo real.', exact: true })
      .waitFor();
    await page.getByText('78 respuestas en esta selección', { exact: true }).waitFor();
    await page
      .getByRole('img', { name: 'Gráfica de tokens de entrada y salida', exact: true })
      .waitFor();
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      true,
      `${name} agent section overflow`,
    );
    await page.screenshot({ path: `test-results/${name}-antigravity.png`, fullPage: true });
    if (name === 'mobile') {
      await page.getByRole('button', { name: 'Mostrar barra lateral', exact: true }).click();
    }
    await page.getByRole('button', { name: 'Vista general', exact: true }).click();
    await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
    // Off-canvas collapsible sidebar: collapse to make the web cleaner, expand on demand.
    if (name === 'desktop') {
      await page.getByRole('button', { name: 'Ocultar barra lateral', exact: true }).click();
      assert.equal(
        await page.evaluate(() => document.querySelector('aside').classList.contains('collapsed')),
        true,
        'desktop sidebar collapsed',
      );
      assert.equal(
        await page.evaluate(() => document.querySelector('.main-wrap').offsetLeft === 0),
        true,
        'desktop main-wrap expanded to full width when sidebar collapsed',
      );
      await page.getByRole('button', { name: 'Mostrar barra lateral', exact: true }).click();
      assert.equal(
        await page.evaluate(() => document.querySelector('aside').classList.contains('open')),
        true,
        'desktop sidebar expanded',
      );
      await page.getByRole('button', { name: 'Cerrar barra lateral', exact: true }).click();
      assert.equal(
        await page.evaluate(() => document.querySelector('aside').classList.contains('collapsed')),
        true,
        'desktop sidebar closed via close button',
      );
      await page.getByRole('button', { name: 'Mostrar barra lateral', exact: true }).click();
    } else {
      assert.equal(
        await page.evaluate(() => document.querySelector('aside').classList.contains('collapsed')),
        true,
        'mobile sidebar starts closed by default',
      );
      await page.getByRole('button', { name: 'Mostrar barra lateral', exact: true }).click();
      assert.equal(
        await page.evaluate(() => document.querySelector('aside').classList.contains('open')),
        true,
        'mobile sidebar opened via toggle button',
      );
      await page.getByRole('button', { name: 'Cerrar barra lateral', exact: true }).click();
      assert.equal(
        await page.evaluate(() => document.querySelector('aside').classList.contains('collapsed')),
        true,
        'mobile sidebar closed offcanvas',
      );
    }
    // External navigation with a Strict cookie must restore the existing session.
    await page.goto('https://usage-monitor-entry-test.example/');
    await page.getByRole('link', { name: 'Abrir AI Usage Monitor' }).click();
    await page.waitForURL(c.origin + '/');
    await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
    // A session can survive reload without any browser storage credentials.
    assert.equal(
      await page.evaluate(() => localStorage.getItem('token') || sessionStorage.getItem('token')),
      null,
    );
    // UI state persists across reload
    assert.equal(await page.evaluate(() => localStorage.getItem('pulse_ui_tab')), 'overview');
    await page.getByLabel('Distribución por').selectOption('agents');
    assert.equal(await page.evaluate(() => localStorage.getItem('pulse_ui_distribution_kind')), 'agents');
    await page.reload();
    await page.getByText('156 respuestas en esta selección', { exact: true }).waitFor();
    assert.equal(await page.evaluate(() => localStorage.getItem('pulse_ui_distribution_kind')), 'agents');
    if (name === 'desktop') {
      await page.getByRole('button', { name: 'Cerrar sesión' }).click();
      await page.waitForURL('**/access');
    } else {
      revoke(db);
      await page.waitForURL('**/access', { timeout: 3000 });
    }
    assert.equal(
      await page.getByRole('heading', { name: 'Cada token, en perspectiva.' }).count(),
      0,
    );
    assert.deepEqual(errors, []);
    report[name] = 'passed';
    await context.close();
  }
  console.log(JSON.stringify(report));
} finally {
  await browser.close();
  collector.stop();
  api.close();
  await Promise.all([new Promise((r) => backend.close(r)), new Promise((r) => proxy.close(r))]);
  db.close();
  rmSync(dir, { recursive: true });
}
