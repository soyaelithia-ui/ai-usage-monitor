import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const __dirname = dirname(fileURLToPath(import.meta.url));
const WEB_DIR = resolve(__dirname, '..');
const ROOT_DIR = resolve(WEB_DIR, '..');
const BINARY_PATH = process.env.USAGE_MONITOR_BIN || join(ROOT_DIR, 'bin', 'ai-usage-monitor');

const CHROME_UA =
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36';

function findChromium() {
  if (process.env.CHROMIUM_PATH && existsSync(process.env.CHROMIUM_PATH)) {
    return process.env.CHROMIUM_PATH;
  }
  const bundled = chromium.executablePath();
  if (bundled && existsSync(bundled)) {
    return bundled;
  }
  const candidates = ['/usr/bin/chromium', '/usr/bin/google-chrome'];
  for (const c of candidates) {
    if (existsSync(c)) return c;
  }
  return undefined;
}

function getFreePort() {
  return new Promise((res, rej) => {
    const srv = createServer();
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      srv.close(() => res(addr.port));
    });
    srv.on('error', rej);
  });
}

async function waitForHealth(url) {
  for (let i = 0; i < 40; i++) {
    try {
      const r = await fetch(url, { headers: { 'User-Agent': CHROME_UA } });
      if (r.ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error('Server did not become healthy in time');
}

function wcagContrastSnippet() {
  return () => {
    const parseColor = (cssColor) => {
      const m = cssColor.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
      if (!m) return null;
      return [Number(m[1]), Number(m[2]), Number(m[3])];
    };
    const relLum = ([r, g, b]) => {
      const lin = [r, g, b].map((c) => {
        const s = c / 255;
        return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
      });
      return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
    };
    const ratio = (c1, c2) => {
      const l1 = relLum(c1);
      const l2 = relLum(c2);
      const hi = Math.max(l1, l2);
      const lo = Math.min(l1, l2);
      return (hi + 0.05) / (lo + 0.05);
    };
    const resolveVar = (varName, prop) => {
      const probe = document.createElement('div');
      probe.style[prop] = `var(${varName})`;
      document.body.appendChild(probe);
      const val = getComputedStyle(probe)[prop];
      probe.remove();
      return parseColor(val);
    };

    const bgCanvas = resolveVar('--bg-canvas', 'backgroundColor');
    const bgPanel = resolveVar('--bg-panel', 'backgroundColor');
    const textPrimary = resolveVar('--text-primary', 'color');
    const textSecondary = resolveVar('--text-secondary', 'color');
    const textMuted = resolveVar('--text-muted', 'color');
    const accentPrimary = resolveVar('--accent-primary', 'backgroundColor');
    const accentPrimaryText = resolveVar('--accent-primary-text', 'color');

    return {
      theme: document.documentElement.getAttribute('data-theme'),
      mode: document.documentElement.getAttribute('data-mode'),
      primaryOnCanvas: ratio(textPrimary, bgCanvas),
      secondaryOnPanel: ratio(textSecondary, bgPanel),
      mutedOnCanvas: ratio(textMuted, bgCanvas),
      mutedOnPanel: ratio(textMuted, bgPanel),
      accentInkOnAccent: ratio(accentPrimaryText, accentPrimary),
    };
  };
}

async function main() {
  assert.ok(existsSync(BINARY_PATH), `Compiled binary not found at ${BINARY_PATH}`);

  const tmpDir = mkdtempSync(join(tmpdir(), 'aum-browser-e2e-'));
  const codexRoot = join(tmpDir, 'codex');
  mkdirSync(codexRoot, { recursive: true });
  const dbPath = join(tmpDir, 'usage.db');
  const port = await getFreePort();
  const ingestKey = 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB';
  const origin = `http://127.0.0.1:${port}`;

  const env = {
    ...process.env,
    PORT: String(port),
    DB_PATH: dbPath,
    ORIGIN: origin,
    INGEST_KEY: ingestKey,
    CODEX_ROOT: codexRoot,
  };

  const issueToken = () => {
    const out = execFileSync(BINARY_PATH, ['issue'], { env, encoding: 'utf8' });
    return JSON.parse(out).token;
  };

  issueToken();
  const proc = spawn(BINARY_PATH, [], { env, stdio: ['ignore', 'pipe', 'pipe'] });
  let stderr = '';
  proc.stderr.on('data', (d) => {
    stderr += d.toString();
  });

  let browser;
  let localProc;
  let localTmpDir;
  let localStderr = '';
  try {
    await waitForHealth(`${origin}/health`);

    // Verify /favicon.svg returns 200 even before authentication
    const favRes = await fetch(`${origin}/favicon.svg`, { headers: { 'User-Agent': CHROME_UA } });
    assert.equal(favRes.status, 200, `Expected GET /favicon.svg to return 200 before login, got ${favRes.status}`);
    const favSvg = await favRes.text();
    assert.ok(favSvg.includes('<svg') && favSvg.includes('#10b981'), 'favicon.svg must be valid emerald SVG');

    const execPath = findChromium();
    browser = await chromium.launch({
      executablePath: execPath,
      headless: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage', `--user-agent=${CHROME_UA}`],
    });

    const authAndLoad = async (viewport, reducedMotion = 'no-preference') => {
      const token = issueToken();
      const ctx = await browser.newContext({ viewport, userAgent: CHROME_UA, reducedMotion });
      const page = await ctx.newPage();
      const pageErrors = [];
      const consoleErrors = [];
      const cspViolations = [];
      const apiRequests = [];

      await page.addInitScript(() => {
        window.__cspViolations = [];
        document.addEventListener('securitypolicyviolation', (e) => {
          window.__cspViolations.push(`${e.violatedDirective}:${e.blockedURI}`);
        });
      });

      page.on('pageerror', (e) => pageErrors.push(e.message));
      page.on('console', (m) => {
        if (m.type() === 'error' && !m.text().includes('401')) {
          consoleErrors.push(m.text());
        }
      });
      page.on('request', (req) => {
        const u = new URL(req.url());
        if (u.pathname.startsWith('/api/') && u.pathname !== '/api/stream') {
          apiRequests.push(u.pathname);
        }
      });

      await page.goto(origin + '/', { waitUntil: 'domcontentloaded' });
      await page.waitForSelector('#access-form', { timeout: 15_000 });
      assert.ok(page.url().endsWith('/access'), `Expected redirect to /access, got ${page.url()}`);

      // Verify favicon link tag points to /favicon.svg (not codex.svg or openai.svg) before login
      const accessIconHref = await page.locator('link[rel="icon"]').getAttribute('href');
      assert.equal(accessIconHref, '/favicon.svg', `Expected link[rel="icon"] to be /favicon.svg, got ${accessIconHref}`);
      assert.ok(!accessIconHref?.includes('codex.svg') && !accessIconHref?.includes('openai.svg'));

      // Invalid token validation
      await page.getByLabel('Token de acceso').fill('invalid-short-token');
      await page.getByRole('button', { name: 'Abrir mi panel' }).click();
      await page.locator('#error').waitFor({ state: 'visible' });
      assert.match(await page.locator('#error').textContent(), /43 caracteres/);

      // Valid token exchange
      await page.getByLabel('Token de acceso').fill(token);
      await page.getByRole('button', { name: 'Abrir mi panel' }).click();
      await page.waitForSelector('.live-dot-inline.on', { timeout: 10_000 });
      assert.equal(new URL(page.url()).pathname, '/');
      const appIconHref = await page.locator('link[rel="icon"]').getAttribute('href');
      assert.equal(appIconHref, '/favicon.svg');

      return { ctx, page, pageErrors, consoleErrors, cspViolations, apiRequests };
    };

    // ------------------------------------------------------------------
    // 1. Desktop E2E: Auth, live SSE ingestion, active-view-only fetching,
    //    unified agent ID, History ARIA, zero inline styles / CSP, zero
    //    tokenizer UI, all 8 themes + WCAG AA, anti-FOUC, tabular-nums, logout
    // ------------------------------------------------------------------
    const { ctx: dCtx, page, pageErrors, consoleErrors, apiRequests } = await authAndLoad({
      width: 1440,
      height: 900,
    });

    // Wait for initial overview load to settle, then clear tracked API requests
    await page.waitForTimeout(250);
    apiRequests.length = 0;

    // Ingest live event via HTTP API + Codex JSONL while SSE is active on Overview tab
    const nowIso = new Date().toISOString();
    const ingRes = await fetch(`${origin}/api/ingest`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${ingestKey}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        schema: 'usage-monitor.usage.v1',
        semantics: 'input_includes_cache;output_includes_reasoning',
        source: 'antigravity',
        provider: 'openai',
        model: 'gpt-6-astra',
        responseId: 'browser-e2e-1',
        sessionId: 'browser-sess-1',
        occurredAt: nowIso,
        inputTokens: '1234',
        outputTokens: '567',
        cachedInputTokens: '200',
        cacheWriteInputTokens: '100',
        reasoningOutputTokens: '50',
      }),
    });
    assert.equal(ingRes.status, 200);

    writeFileSync(
      join(codexRoot, 'live.jsonl'),
      JSON.stringify({ type: 'turn_context', payload: { turn_id: 'bt1', model: 'gpt-6.1-sol' } }) +
        '\n' +
        JSON.stringify({
          type: 'token_usage_record',
          timestamp: new Date(Date.now() + 10).toISOString(),
          payload: {
            response_id: 'browser-codex-1',
            session_id: 'browser-sess-2',
            turn_id: 'bt1',
            usage: {
              input_tokens: 800,
              output_tokens: 150,
              cached_input_tokens: 100,
              cache_write_input_tokens: 0,
              reasoning_output_tokens: 20,
            },
          },
        }) +
        '\n',
    );

    // Wait for live SSE UI update without reload
    await page.waitForFunction(
      () => {
        const txt = document.body.innerText;
        return txt.includes('2 eventos') && txt.includes('Antigravity') && txt.includes('Codex');
      },
      { timeout: 10_000 },
    );

    // Verify separate pricing note paragraph is NOT rendered in Overview DOM
    const overviewBodyText = await page.locator('main.workspace').innerText();
    assert.equal(
      overviewBodyText.includes('Equivalente API Standard, USD; nivel supuesto'),
      false,
      'Overview DOM must NOT render separate pricing note paragraph',
    );
    assert.equal(
      await page.locator('main.workspace p.notice').count(),
      0,
      'Overview DOM must NOT render separate <p class="notice"> pricing paragraph',
    );

    // Active-view-only check on Overview SSE updates: ONLY /api/analytics should have been called
    assert.ok(apiRequests.includes('/api/analytics'), `Expected /api/analytics on SSE, got ${apiRequests}`);
    for (const forbidden of ['/api/events', '/api/discovery', '/api/reconciliation', '/api/sources']) {
      assert.ok(
        !apiRequests.includes(forbidden),
        `Overview SSE over-fetched inactive endpoint ${forbidden}: ${apiRequests.join(', ')}`,
      );
    }

    // Active-view-only check on Overview filter change (default window is 1h -> switch to 24h)
    apiRequests.length = 0;
    await page.locator('.segments button', { hasText: '24h' }).click();
    await page.waitForTimeout(200);
    assert.deepEqual(
      Array.from(new Set(apiRequests)),
      ['/api/analytics'],
      `Overview filter change must only call /api/analytics, got ${apiRequests.join(', ')}`,
    );

    // Zero inline style attributes across all 3 tabs + zero tokenizer UI controls + zero horizontal overflow
    for (const tabLabel of ['Vista general', 'Historial', 'Fuentes y lectores']) {
      apiRequests.length = 0;
      await page.locator('aside nav button', { hasText: tabLabel }).click();
      await page.waitForTimeout(200);

      const domAudit = await page.evaluate(() => ({
        overflow: document.documentElement.scrollWidth > window.innerWidth,
        inlineStyles: document.querySelectorAll('[style]').length,
        hasTokenizerUI: /tokenizador/i.test(document.body.innerText),
      }));
      assert.equal(domAudit.overflow, false, `Desktop horizontal overflow detected on tab ${tabLabel}`);
      assert.equal(domAudit.inlineStyles, 0, `Inline style attribute detected on tab ${tabLabel}`);
      assert.equal(domAudit.hasTokenizerUI, false, `Tokenizer UI text leaked on tab ${tabLabel}`);

      if (tabLabel === 'Historial') {
        assert.deepEqual(
          Array.from(new Set(apiRequests)),
          ['/api/events'],
          `History tab switch must only call /api/events, got ${apiRequests.join(', ')}`,
        );
      } else if (tabLabel === 'Fuentes y lectores') {
        assert.ok(!apiRequests.includes('/api/analytics'), 'Sources tab must not fetch /api/analytics');
        assert.ok(!apiRequests.includes('/api/events'), 'Sources tab must not fetch /api/events');
        assert.ok(apiRequests.includes('/api/sources') && apiRequests.includes('/api/discovery') && apiRequests.includes('/api/reconciliation'));
      }
    }

    // Verify /api/sources payload contains no tokenizer key
    const sourcesJson = await page.evaluate(async () => {
      const r = await fetch('/api/sources', { credentials: 'same-origin' });
      return r.json();
    });
    assert.equal('tokenizer' in sourcesJson, false, '/api/sources must never expose tokenizer key');

    // Verify History ARIA semantics (ul.history-list > li.history-item > details.event-disclosure, ZERO role="table")
    await page.locator('aside nav button', { hasText: 'Historial' }).click();
    const firstDetails = page.locator('ul.history-list > li.history-item > details.event-disclosure').first();
    await firstDetails.waitFor({ state: 'visible' });
    const invalidTableRoleCount = await page.locator('[role="table"] details, details [role="table"]').count();
    assert.equal(invalidTableRoleCount, 0, 'History <details> must never be wrapped in role="table"');
    assert.equal(await firstDetails.getAttribute('open'), null);
    await firstDetails.locator('summary.event-summary').click();
    assert.notEqual(await firstDetails.getAttribute('open'), null);
    const detailBox = await firstDetails.locator('.event-detail').boundingBox();
    assert.ok(detailBox && detailBox.height > 40, 'Expanded <details> row must have visible detail height');

    // Verify unified canonical agent ID across Sidebar -> Overview <select> -> History <select>
    await page.locator('aside nav button', { hasText: 'Antigravity' }).click();
    await page.waitForTimeout(150);
    const overviewSelectVal = await page.locator('select[aria-label="Filtrar por agente"]').inputValue();
    assert.equal(overviewSelectVal, 'antigravity', 'Sidebar click must sync canonical agent ID to Overview select');

    await page.locator('aside nav button', { hasText: 'Historial' }).click();
    await page.waitForTimeout(150);
    const historySelectVal = await page.locator('select[aria-label="Filtrar por agente"]').inputValue();
    assert.equal(historySelectVal, 'antigravity', 'Canonical agent ID must remain synced in History select');
    const filteredCount = await page.locator('ul.history-list > li.history-item').count();
    assert.equal(filteredCount, 1, 'Filtered History must show only 1 Antigravity event');

    // Clear agent filter & return to overview
    await page.locator('select[aria-label="Filtrar por agente"]').selectOption('');
    await page.locator('aside nav button', { hasText: 'Vista general' }).click();
    await page.waitForTimeout(150);

    // Check zero CSP violations before running temporary inline style measurement probes
    const cspBeforeProbes = await page.evaluate(() => window.__cspViolations || []);
    assert.deepEqual(cspBeforeProbes, [], `CSP violations detected: ${cspBeforeProbes.join('; ')}`);

    // Verify tabular-nums zero-jitter & font-kerning: none
    const jitterReport = await page.evaluate(async () => {
      await document.fonts.ready;
      const rootKerning = getComputedStyle(document.documentElement).fontKerning;
      const statEl = document.querySelector('.stat > strong');
      const statStyle = getComputedStyle(statEl);
      const probe = document.createElement('span');
      probe.className = 'tabular-nums';
      probe.style.fontFamily = statStyle.fontFamily;
      probe.style.fontSize = statStyle.fontSize;
      probe.style.fontWeight = statStyle.fontWeight;
      probe.style.position = 'absolute';
      probe.style.visibility = 'hidden';
      document.body.appendChild(probe);

      const widths = [];
      for (const sample of ['111,111', '888,888', '000,000', '444,444']) {
        probe.textContent = sample;
        widths.push(probe.getBoundingClientRect().width);
      }
      probe.remove();
      return {
        rootKerning,
        fontVariantNumeric: statStyle.fontVariantNumeric,
        minW: Math.min(...widths),
        maxW: Math.max(...widths),
      };
    });
    assert.equal(jitterReport.rootKerning, 'none');
    assert.ok(jitterReport.fontVariantNumeric.includes('tabular-nums'));
    assert.ok(
      jitterReport.maxW - jitterReport.minW < 0.5,
      `Expected zero tabular-nums jitter, got widths ${jitterReport.minW}..${jitterReport.maxW}`,
    );

    // Keyboard focus indicator check
    await page.locator('header button[aria-label="Seleccionar tema"]').focus();
    await page.keyboard.press('Tab');
    const focusInfo = await page.evaluate(() => {
      const el = document.activeElement;
      const cs = getComputedStyle(el);
      return {
        tag: el?.tagName,
        outlineStyle: cs.outlineStyle,
        outlineWidth: parseFloat(cs.outlineWidth || '0'),
      };
    });
    assert.notEqual(focusInfo.outlineStyle, 'none', 'Keyboard focus outline must be visible');
    assert.ok(focusInfo.outlineWidth >= 2, `Expected outlineWidth >= 2px, got ${focusInfo.outlineWidth}`);

    // Audit all 4 themes x 2 modes (8 combinations) for real WCAG AA contrast + anti-FOUC persistence
    const themeLabels = ['Obsidiana', 'Zafiro', 'Ámbar', 'Esmeralda'];
    for (const label of themeLabels) {
      await page.locator('button[aria-label="Seleccionar tema"]').click();
      await page.locator('.theme-menu-item', { hasText: label }).click();

      for (let m = 0; m < 2; m++) {
        const res = await page.evaluate(wcagContrastSnippet());
        assert.ok(res.primaryOnCanvas >= 7.0, `${res.theme}/${res.mode} primaryOnCanvas=${res.primaryOnCanvas.toFixed(2)} < 7.0`);
        assert.ok(res.secondaryOnPanel >= 4.5, `${res.theme}/${res.mode} secondaryOnPanel=${res.secondaryOnPanel.toFixed(2)} < 4.5`);
        assert.ok(res.mutedOnCanvas >= 4.5, `${res.theme}/${res.mode} mutedOnCanvas=${res.mutedOnCanvas.toFixed(2)} < 4.5`);
        assert.ok(res.mutedOnPanel >= 4.5, `${res.theme}/${res.mode} mutedOnPanel=${res.mutedOnPanel.toFixed(2)} < 4.5`);
        assert.ok(res.accentInkOnAccent >= 4.5, `${res.theme}/${res.mode} accentInkOnAccent=${res.accentInkOnAccent.toFixed(2)} < 4.5`);

        // Toggle mode for second pass
        await page.locator('button.mode-toggle').click();
      }
    }

    // Select a non-default theme & light mode, reload, and verify anti-FOUC persistence
    await page.locator('button[aria-label="Seleccionar tema"]').click();
    await page.locator('.theme-menu-item', { hasText: 'Ámbar' }).click();
    if ((await page.evaluate(() => document.documentElement.getAttribute('data-mode'))) !== 'light') {
      await page.locator('button.mode-toggle').click();
    }
    await page.reload({ waitUntil: 'domcontentloaded' });
    const persisted = await page.evaluate(() => ({
      theme: document.documentElement.getAttribute('data-theme'),
      mode: document.documentElement.getAttribute('data-mode'),
    }));
    assert.deepEqual(persisted, { theme: 'burned-paper', mode: 'light' });

    // Logout via UI and verify redirect to /access + telemetry cleared
    await page.locator('button[aria-label="Cerrar sesión"]').click();
    await page.waitForSelector('#access-form', { timeout: 5_000 });
    assert.ok(page.url().endsWith('/access'));
    assert.equal(await page.locator('.stat').count(), 0);
    assert.deepEqual(pageErrors, [], `Desktop pageErrors: ${pageErrors.join('; ')}`);
    assert.deepEqual(consoleErrors, [], `Desktop consoleErrors: ${consoleErrors.join('; ')}`);
    await dCtx.close();

    // ------------------------------------------------------------------
    // 2. Mobile viewport (390x844) + prefers-reduced-motion: reduce audit
    // ------------------------------------------------------------------
    const { ctx: mCtx, page: mPage, pageErrors: mPE, consoleErrors: mCE } = await authAndLoad(
      { width: 390, height: 844 },
      'reduce',
    );

    // Mobile drawer toggle check
    await mPage.locator('header button[aria-label="Alternar navegación"]').click();
    await mPage.waitForSelector('.app-shell.mobile-drawer-open');
    await mPage.locator('aside button[aria-label="Cerrar menú"]').click();

    // Mobile tabs navigation + zero horizontal overflow + <details> expansion
    for (const tabText of ['Resumen', 'Historial', 'Fuentes']) {
      await mPage.locator('.mobile-tabs button', { hasText: tabText }).click();
      await mPage.waitForTimeout(80);
      const overflow = await mPage.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
      assert.equal(overflow, false, `Mobile horizontal overflow on tab ${tabText}`);
    }

    await mPage.locator('.mobile-tabs button', { hasText: 'Historial' }).click();
    const mDetails = mPage.locator('ul.history-list > li.history-item > details.event-disclosure').first();
    await mDetails.waitFor({ state: 'visible' });
    await mDetails.locator('summary.event-summary').click();
    assert.notEqual(await mDetails.getAttribute('open'), null);

    // Verify prefers-reduced-motion suppression
    const rmCheck = await mPage.evaluate(() => {
      const btn = document.querySelector('header button');
      const tab = document.querySelector('.tab-content');
      const btnStyle = getComputedStyle(btn);
      const tabStyle = getComputedStyle(tab);
      return {
        matches: window.matchMedia('(prefers-reduced-motion: reduce)').matches,
        btnTransitionDuration: btnStyle.transitionDuration,
        tabAnimationName: tabStyle.animationName,
        tabOpacity: tabStyle.opacity,
      };
    });
    assert.equal(rmCheck.matches, true);
    assert.equal(rmCheck.btnTransitionDuration, '0s');
    assert.equal(rmCheck.tabAnimationName, 'none');
    assert.equal(rmCheck.tabOpacity, '1');

    // Verify live server-side revocation closes SSE and returns browser to /access
    execFileSync(BINARY_PATH, ['revoke'], { env });
    await mPage.waitForSelector('#access-form', { timeout: 5_000 });
    assert.ok(mPage.url().endsWith('/access'));
    assert.equal(await mPage.locator('.stat').count(), 0);

    assert.deepEqual(mPE, [], `Mobile pageErrors: ${mPE.join('; ')}`);
    assert.deepEqual(mCE, [], `Mobile consoleErrors: ${mCE.join('; ')}`);
    await mCtx.close();

    // ------------------------------------------------------------------
    // 3. Phase 2a: Permanent/reusable token works across two separate browser contexts
    // ------------------------------------------------------------------
    const permOut = execFileSync(BINARY_PATH, ['issue', '--permanent'], { env, encoding: 'utf8' });
    const permPayload = JSON.parse(permOut);
    const permToken = permPayload.token;
    assert.ok(permToken && permToken.length === 43, 'issue --permanent must emit a 43-char token');
    assert.equal(permPayload.permanent, true, 'issue --permanent must report permanent: true');

    const permCtx1 = await browser.newContext({ viewport: { width: 1440, height: 900 }, userAgent: CHROME_UA });
    const permPage1 = await permCtx1.newPage();
    await permPage1.goto(origin + '/', { waitUntil: 'domcontentloaded' });
    await permPage1.waitForSelector('#access-form', { timeout: 10_000 });
    await permPage1.getByLabel('Token de acceso').fill(permToken);
    await permPage1.getByRole('button', { name: 'Abrir mi panel' }).click();
    await permPage1.waitForSelector('.live-dot-inline.on', { timeout: 10_000 });
    assert.equal(new URL(permPage1.url()).pathname, '/');
    assert.equal(await permPage1.locator('button[aria-label="Cerrar sesión"]').count(), 1);

    const permCtx2 = await browser.newContext({ viewport: { width: 1440, height: 900 }, userAgent: CHROME_UA });
    const permPage2 = await permCtx2.newPage();
    await permPage2.goto(origin + '/', { waitUntil: 'domcontentloaded' });
    await permPage2.waitForSelector('#access-form', { timeout: 10_000 });
    await permPage2.getByLabel('Token de acceso').fill(permToken);
    await permPage2.getByRole('button', { name: 'Abrir mi panel' }).click();
    await permPage2.waitForSelector('.live-dot-inline.on', { timeout: 10_000 });
    assert.equal(new URL(permPage2.url()).pathname, '/');
    await permPage2.waitForFunction(() => document.body.innerText.includes('2 eventos'), { timeout: 10_000 });

    await permPage1.reload({ waitUntil: 'domcontentloaded' });
    await permPage1.waitForSelector('.live-dot-inline.on', { timeout: 10_000 });
    await permCtx1.close();
    await permCtx2.close();

    // ------------------------------------------------------------------
    // 4. Phase 2b: Local tokenless mode (AUTH_MODE=local)
    // ------------------------------------------------------------------
    localTmpDir = mkdtempSync(join(tmpdir(), 'aum-local-e2e-'));
    const localCodexRoot = join(localTmpDir, 'codex');
    mkdirSync(localCodexRoot, { recursive: true });
    const localPort = await getFreePort();
    const localOrigin = `http://127.0.0.1:${localPort}`;
    const localEnv = {
      ...process.env,
      PORT: String(localPort),
      BIND_ADDR: '127.0.0.1',
      AUTH_MODE: 'local',
      DB_PATH: join(localTmpDir, 'usage.db'),
      ORIGIN: localOrigin,
      INGEST_KEY: ingestKey,
      CODEX_ROOT: localCodexRoot,
    };

    localProc = spawn(BINARY_PATH, [], { env: localEnv, stdio: ['ignore', 'pipe', 'pipe'] });
    localProc.stderr.on('data', (d) => {
      localStderr += d.toString();
    });
    await waitForHealth(`${localOrigin}/health`);

    const localCtx = await browser.newContext({ viewport: { width: 1440, height: 900 }, userAgent: CHROME_UA });
    const localPage = await localCtx.newPage();
    const localPageErrors = [];
    const localConsoleErrors = [];

    await localPage.addInitScript(() => {
      window.__cspViolations = [];
      document.addEventListener('securitypolicyviolation', (e) => {
        window.__cspViolations.push(`${e.violatedDirective}:${e.blockedURI}`);
      });
    });
    localPage.on('pageerror', (e) => localPageErrors.push(e.message));
    localPage.on('console', (m) => {
      if (m.type() === 'error') {
        localConsoleErrors.push(m.text());
      }
    });

    // Loads / directly without redirecting to /access and hides logout button
    await localPage.goto(localOrigin + '/', { waitUntil: 'domcontentloaded' });
    await localPage.waitForSelector('.live-dot-inline.on', { timeout: 15_000 });
    assert.equal(new URL(localPage.url()).pathname, '/');
    assert.equal(await localPage.locator('#access-form').count(), 0, 'Local mode must not show #access-form');
    assert.equal(
      await localPage.locator('button[aria-label="Cerrar sesión"]').count(),
      0,
      'Local mode must hide logout button',
    );
    assert.equal(await localPage.locator('link[rel="icon"]').getAttribute('href'), '/favicon.svg');

    // Streams SSE and renders telemetry cleanly
    const localIngRes = await fetch(`${localOrigin}/api/ingest`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${ingestKey}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        schema: 'usage-monitor.usage.v1',
        semantics: 'input_includes_cache;output_includes_reasoning',
        source: 'antigravity',
        provider: 'openai',
        model: 'gpt-6-astra',
        responseId: 'local-e2e-1',
        sessionId: 'local-sess-1',
        occurredAt: new Date().toISOString(),
        inputTokens: '400',
        outputTokens: '120',
        cachedInputTokens: '50',
        cacheWriteInputTokens: '0',
        reasoningOutputTokens: '10',
      }),
    });
    assert.equal(localIngRes.status, 200);

    await localPage.waitForFunction(
      () => {
        const txt = document.body.innerText;
        return txt.includes('1 evento') && txt.includes('Antigravity');
      },
      { timeout: 10_000 },
    );

    const localOverviewText = await localPage.locator('main.workspace').innerText();
    assert.equal(
      localOverviewText.includes('Equivalente API Standard, USD; nivel supuesto'),
      false,
      'Local mode Overview must NOT render separate pricing note paragraph',
    );
    assert.equal(await localPage.locator('main.workspace p.notice').count(), 0);
    assert.equal(await localPage.evaluate(() => document.querySelectorAll('[style]').length), 0);
    assert.deepEqual(await localPage.evaluate(() => window.__cspViolations || []), []);

    // Navigating to /access in local mode also resolves directly to /
    await localPage.goto(localOrigin + '/access', { waitUntil: 'domcontentloaded' });
    await localPage.waitForSelector('.live-dot-inline.on', { timeout: 10_000 });
    assert.equal(new URL(localPage.url()).pathname, '/');
    assert.equal(await localPage.locator('#access-form').count(), 0);

    assert.deepEqual(localPageErrors, [], `Local mode pageErrors: ${localPageErrors.join('; ')}`);
    assert.deepEqual(localConsoleErrors, [], `Local mode consoleErrors: ${localConsoleErrors.join('; ')}`);
    await localCtx.close();

    console.log(
      'BROWSER E2E AUDIT PASSED: Desktop (1440x900), Mobile (390x844), /favicon.svg, no separate pricing note paragraph, permanent token reuse, local tokenless mode, active-view-only fetches, unified agent ID, semantic History ARIA, zero inline styles/CSP violations, zero tokenizer UI, 8 theme/mode WCAG AA combos, anti-FOUC, SSE live update, <details>, tabular-nums, keyboard focus, reduced-motion, and logout/revocation.',
    );
  } catch (err) {
    console.error('Server stderr:\n' + stderr);
    if (localStderr) console.error('Local server stderr:\n' + localStderr);
    throw err;
  } finally {
    const stopChild = async (child) => {
      if (!child || child.exitCode !== null || child.signalCode !== null) return;
      child.kill('SIGTERM');
      await new Promise((r) => (child.exitCode !== null || child.signalCode !== null ? r() : child.once('exit', r)));
    };
    if (browser) await browser.close().catch(() => {});
    await stopChild(localProc);
    await stopChild(proc);
    rmSync(tmpDir, { recursive: true, force: true });
    if (localTmpDir) rmSync(localTmpDir, { recursive: true, force: true });
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
