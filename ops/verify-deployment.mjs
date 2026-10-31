// Uses a short-lived independent test access. Never issues/revokes the user's access.
import assert from 'node:assert/strict';
import { readFileSync, mkdirSync } from 'node:fs';
import { chromium } from '@playwright/test';
import { config } from '../server/config.js';
import { openDb, hash, secret } from '../server/db.js';
const c = config(),
  db = openDb(c.db);
const proof = (table) =>
  hash(JSON.stringify(db.prepare('SELECT * FROM ' + table + ' ORDER BY hash').all()));
const beforeAccesses = proof('accesses'),
  beforeSessions = proof('sessions');
const token = secret(),
  accessHash = hash(token);
let sessionHash, browser, reader;
const report = {};
try {
  for (const path of [
    '/api/analytics',
    '/api/events',
    '/api/sources',
    '/api/reconciliation',
    '/api/stream',
  ])
    assert.equal((await fetch(c.origin + path)).status, 401);
  assert.equal(
    (
      await fetch(c.origin + '/api/ingest', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      })
    ).status,
    404,
  );
  db.prepare('INSERT INTO accesses(hash,expires) VALUES (?,?)').run(
    accessHash,
    Date.now() + 120000,
  );
  const exchange = () =>
    fetch(c.origin + '/auth/exchange', {
      method: 'POST',
      headers: { origin: c.origin, 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    });
  const login = await exchange();
  assert.equal(login.status, 200);
  const session = await login.json(),
    cookie = login.headers.get('set-cookie').split(';')[0],
    raw = cookie.slice(cookie.indexOf('=') + 1);
  sessionHash = hash(raw);
  assert.equal((await exchange()).status, 401);
  const headers = { cookie };
  const data = await (await fetch(c.origin + '/api/analytics?window=24h', { headers })).json();
  const sources = await (await fetch(c.origin + '/api/sources', { headers })).json(),
    reconciliation = await (
      await fetch(c.origin + '/api/reconciliation?window=24h', { headers })
    ).json();
  assert.ok(data.agentPanels.every((panel) => typeof panel.name === 'string'));
  assert.equal(typeof reconciliation.conflicts, 'number');
  report.https = true;
  report.anonymousDenied = true;
  report.sources = sources;
  report.reconciliation = {
    responses: reconciliation.responses,
    matched: reconciliation.matched,
    conflicts: reconciliation.conflicts,
  };
  const stream = await fetch(c.origin + '/api/stream', { headers });
  reader = stream.body.getReader();
  assert.match(new TextDecoder().decode((await reader.read()).value), /ready/);
  browser = await chromium.launch({
    executablePath: process.env.CHROMIUM_PATH || undefined,
    headless: true,
    chromiumSandbox: process.env.CHROMIUM_NO_SANDBOX !== '1',
  });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  await context.addCookies([
    {
      name: '__Host-usage-monitor',
      value: raw,
      url: c.origin,
      httpOnly: true,
      secure: true,
      sameSite: 'Strict',
    },
  ]);
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(c.origin);
  await page.getByRole('heading', { name: 'Cada token, en perspectiva.' }).waitFor();
  await page.getByRole('button', { name: 'Fuentes y precisión', exact: true }).click();
  await page.getByRole('heading', { name: 'Fuentes y precisión' }).waitFor();
  await page.getByRole('button', { name: 'Vista general', exact: true }).click();
  await page.getByRole('button', { name: '24 horas', exact: true }).click();
  await page
    .getByText('Equivalencia de API, no gasto de tu suscripción.', { exact: true })
    .waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  assert.equal(await page.evaluate(() => localStorage.getItem('token') || sessionStorage.getItem('token')), null);
  assert.equal(await page.evaluate(() => localStorage.getItem('pulse_ui_window')), '24h');
  mkdirSync('test-results', { recursive: true });
  await page.screenshot({ path: 'test-results/production-desktop.png', fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.screenshot({ path: 'test-results/production-mobile.png', fullPage: true });
  assert.deepEqual(errors, []);
  const agentChecks = [];
  for (const name of ['Codex', 'Antigravity', 'OpenCode']) {
    const toggle = page.getByRole('button', { name: 'Mostrar barra lateral', exact: true });
    if (await toggle.isVisible()) {
      await toggle.click();
    }
    await page.getByRole('button', { name: `Apartado ${name}`, exact: true }).click();
    await page.getByRole('heading', { name: `${name}, en tiempo real.`, exact: true }).waitFor();
    await page.getByRole('region', { name: `Estado de ${name}`, exact: true }).waitFor();
    await page
      .getByRole('img', { name: 'Gráfica de tokens de entrada y salida', exact: true })
      .waitFor();
    const selected = await (
      await fetch(c.origin + '/api/analytics?window=24h&agent=' + encodeURIComponent(name), {
        headers,
      })
    ).json();
    assert.ok(selected.agentPanels.every((panel) => panel.name === name));
    assert.equal(
      await page
        .locator('tbody .agent-badge')
        .evaluateAll((nodes, agent) => nodes.every((n) => n.textContent === agent), name),
      true,
    );
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      true,
    );
    if (name === 'Antigravity')
      await page.screenshot({ path: 'test-results/production-agent-antigravity.png' });
    agentChecks.push({ agent: name, responses: selected.totals.count });
  }
  report.agentSections = agentChecks;
  const logout = await fetch(c.origin + '/auth/logout', {
    method: 'POST',
    headers: {
      cookie,
      origin: c.origin,
      'Content-Type': 'application/json',
      'X-CSRF-Token': session.csrf,
    },
    body: '{}',
  });
  assert.equal(logout.status, 200);
  let expired = false;
  for (let i = 0; i < 10; i++) {
    const chunk = await Promise.race([
      reader.read(),
      new Promise((_, reject) =>
        setTimeout(() => reject(Error('SSE expiry timeout')), 3000).unref(),
      ),
    ]);
    if (chunk.done) break;
    if (new TextDecoder().decode(chunk.value).includes('expired')) {
      expired = true;
      break;
    }
  }
  assert.ok(expired);
  await reader.cancel();
  reader = null;
  await page.waitForURL('**/access', { timeout: 5000 });
  report.browser = 'desktop and mobile passed';
  report.testSessionRevoked = true;
} finally {
  if (reader) await reader.cancel();
  if (browser) await browser.close();
  db.transaction(() => {
    db.prepare('DELETE FROM accesses WHERE hash=?').run(accessHash);
    if (sessionHash) db.prepare('DELETE FROM sessions WHERE hash=?').run(sessionHash);
  }).immediate();
  assert.equal(proof('accesses'), beforeAccesses);
  assert.equal(proof('sessions'), beforeSessions);
  report.userAccessPreserved = true;
  db.close();
}
console.log(JSON.stringify(report, null, 2));
