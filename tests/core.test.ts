import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { calculateCost, catalogSchema, loadPricing } from '../server/pricing.js';
import { ConflictError, UsageStore } from '../server/store.js';
import type { UsageInput } from '../shared/types.js';

const catalog = loadPricing('config/pricing.json');
const now = '2026-10-28T12:00:00.000Z';
const input: UsageInput = { agent_name: 'investigador', model: 'gpt-6.1-sol', prompt_tokens: 1000, completion_tokens: 500 };

test('costos exactos, cero, modelos desconocidos y nombres heredados', () => {
  assert.equal(calculateCost(input, catalog, now).cost_usd, '0.007');
  assert.equal(calculateCost({ ...input, prompt_tokens: 0, completion_tokens: 0 }, catalog, now).cost_usd, '0');
  for (const model of ['desconocido', '__proto__', 'constructor']) {
    assert.equal(calculateCost({ ...input, model }, catalog, now).cost_usd, null);
  }
});

test('el recargo de contexto se aplica al superar el límite y a toda la petición', () => {
  for (const [model, expectedBase, expectedLong] of [
    ['gpt-6.1-sol', '0.554', '1.103004'],
    ['gpt-6-luna', '0.0277', '0.0551502'],
  ]) {
    const atLimit = calculateCost({ ...input, model, prompt_tokens: 272000, completion_tokens: 1000 }, catalog, now);
    const above = calculateCost({ ...input, model, prompt_tokens: 272001, completion_tokens: 1000 }, catalog, now);
    assert.equal(atLimit.cost_usd, expectedBase);
    assert.equal(atLimit.pricing!.long_context, false);
    assert.equal(above.cost_usd, expectedLong);
    assert.equal(above.pricing!.long_context, true);
  }
});

test('las tarifas programadas cambian exactamente a medianoche UTC', () => {
  const gemini = { ...input, model: 'gemini-3.8-flash', prompt_tokens: 1_000_000, completion_tokens: 1_000_000 };
  assert.equal(calculateCost(gemini, catalog, '2026-12-31T23:59:59.999Z').cost_usd, '4.5');
  assert.equal(calculateCost(gemini, catalog, '2027-01-01T00:00:00.000Z').cost_usd, '9');
  assert.equal(calculateCost(input, catalog, '2020-01-01T00:00:00.000Z').cost_usd, null);
});

test('catálogo rechaza tarifas negativas, fechas inválidas y fechas repetidas', () => {
  const invalid = structuredClone(catalog);
  invalid.models['gpt-6.1-sol'].rates[0].input_per_million = '-1';
  assert.equal(catalogSchema.safeParse(invalid).success, false);
  invalid.models['gpt-6.1-sol'].rates[0].input_per_million = '2';
  invalid.verified_at = '2026-02-30';
  assert.equal(catalogSchema.safeParse(invalid).success, false);
  invalid.verified_at = '2026-10-28';
  invalid.models['gpt-6.1-sol'].rates.push(invalid.models['gpt-6.1-sol'].rates[0]);
  assert.equal(catalogSchema.safeParse(invalid).success, false);
});

test('acumulación decimal, agrupaciones e idempotencia sin doble conteo', () => {
  const store = new UsageStore(':memory:', catalog);
  try {
    for (let index = 0; index < 1000; index++) {
      store.ingest({ ...input, model: 'gpt-6-luna', prompt_tokens: 1, completion_tokens: 0, agent_name: index % 2 ? 'a' : 'b' }, now);
    }
    assert.equal(store.summary().totals.cost_usd, '0.0001');
    assert.equal(store.summary().totals.prompt_tokens, '1000');
    const first = store.ingest({ ...input, request_id: 'abc' }, now);
    const duplicate = store.ingest({ ...input, request_id: 'abc' }, '2027-01-01T00:00:00.000Z');
    assert.deepEqual(duplicate.event, first.event);
    assert.equal(duplicate.duplicate, true);
    assert.throws(() => store.ingest({ ...input, request_id: 'abc', prompt_tokens: 0 }, now), ConflictError);
    store.ingest({ ...input, model: 'no-price' }, now);
    const summary = store.summary();
    assert.equal(summary.totals.requests, 1002);
    assert.equal(summary.totals.cost_usd, '0.0071');
    assert.equal(summary.totals.unpriced_requests, 1);
    assert.equal(summary.agents.find(group => group.name === 'a')!.prompt_tokens, '500');
    assert.equal(summary.models.find(group => group.name === 'no-price')!.unpriced_requests, 1);
  } finally { store.close(); }
});

test('totales de tokens mantienen precisión por encima de MAX_SAFE_INTEGER', () => {
  const store = new UsageStore(':memory:', catalog);
  try {
    for (let index = 0; index < 3; index++) store.ingest({ ...input, prompt_tokens: Number.MAX_SAFE_INTEGER }, now);
    assert.equal(store.summary().totals.prompt_tokens, '27021597764222973');
  } finally { store.close(); }
});

test('reiniciar conserva costos históricos y precios nuevos solo afectan eventos nuevos', () => {
  const directory = mkdtempSync(join(tmpdir(), 'pulse-store-'));
  const path = join(directory, 'usage.sqlite');
  let store = new UsageStore(path, catalog);
  try {
    const old = store.ingest(input, now).event;
    store.close();
    const nextCatalog = structuredClone(catalog);
    nextCatalog.models['gpt-6.1-sol'].rates[0].input_per_million = '100';
    store = new UsageStore(path, nextCatalog);
    assert.deepEqual(store.events().events[0], old);
    assert.equal(store.summary().totals.cost_usd, '0.007');
    const next = store.ingest(input, now).event;
    assert.equal(next.cost_usd, '0.105');
    assert.equal(store.summary().totals.cost_usd, '0.112');
  } finally { store.close(); rmSync(directory, { recursive: true, force: true }); }
});

test('paginación por cursor permanece estable con nuevas inserciones', () => {
  const store = new UsageStore(':memory:', catalog);
  try {
    for (let index = 0; index < 105; index++) store.ingest(input, now);
    const first = store.events();
    assert.equal(first.events.length, 100);
    assert.equal(first.next_before_id, 6);
    store.ingest(input, now);
    const older = store.events(first.next_before_id!);
    assert.deepEqual(older.events.map(event => event.id), [5, 4, 3, 2, 1]);
    assert.equal(older.next_before_id, null);
  } finally { store.close(); }
});
