import Database from 'better-sqlite3';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { Decimal } from 'decimal.js';
import type { EventPage, Group, PricingCatalog, Snapshot, Summary, Totals, UsageEvent, UsageInput } from '../shared/types.js';
import { calculateCost } from './pricing.js';

type Row = Omit<UsageEvent, 'pricing'> & { pricing_json: string | null };
type Accumulator = { requests: number; input: bigint; output: bigint; cost: Decimal; unpriced: number };
const empty = (): Accumulator => ({ requests: 0, input: 0n, output: 0n, cost: new Decimal(0), unpriced: 0 });
const fromRow = ({ pricing_json, ...row }: Row): UsageEvent => ({
  ...row, pricing: pricing_json ? JSON.parse(pricing_json) : null,
});
function accumulate(target: Accumulator, event: UsageEvent) {
  target.requests++;
  target.input += BigInt(event.prompt_tokens);
  target.output += BigInt(event.completion_tokens);
  if (event.cost_usd === null) target.unpriced++;
  else target.cost = target.cost.plus(event.cost_usd);
}
const serialize = (value: Accumulator): Totals => ({
  requests: value.requests,
  prompt_tokens: String(value.input),
  completion_tokens: String(value.output),
  total_tokens: String(value.input + value.output),
  cost_usd: value.cost.toFixed(),
  unpriced_requests: value.unpriced,
});

export class ConflictError extends Error {}

export class UsageStore {
  private db: Database.Database;
  private totals = empty();
  private agents = new Map<string, Accumulator>();
  private models = new Map<string, Accumulator>();
  private lastId = 0;

  constructor(path: string, private catalog: PricingCatalog) {
    if (path !== ':memory:') mkdirSync(dirname(path), { recursive: true });
    this.db = new Database(path);
    this.db.pragma('journal_mode = WAL');
    this.db.pragma('busy_timeout = 5000');
    const version = this.db.pragma('user_version', { simple: true }) as number;
    if (version > 1) { this.db.close(); throw new Error('Versión de base de datos no compatible'); }
    this.db.transaction(() => {
      this.db.exec(`CREATE TABLE IF NOT EXISTS usage_events (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        request_id TEXT UNIQUE,
        received_at TEXT NOT NULL,
        agent_name TEXT NOT NULL,
        model TEXT NOT NULL,
        prompt_tokens INTEGER NOT NULL CHECK(prompt_tokens >= 0),
        completion_tokens INTEGER NOT NULL CHECK(completion_tokens >= 0),
        input_cost_usd TEXT,
        output_cost_usd TEXT,
        cost_usd TEXT,
        pricing_json TEXT
      ); PRAGMA user_version = 1;`);
    })();
    for (const row of this.db.prepare('SELECT * FROM usage_events ORDER BY id').iterate() as Iterable<Row>) {
      this.addToTotals(fromRow(row));
    }
  }

  private addToTotals(event: UsageEvent) {
    accumulate(this.totals, event);
    for (const [map, key] of [[this.agents, event.agent_name], [this.models, event.model]] as const) {
      if (!map.has(key)) map.set(key, empty());
      accumulate(map.get(key)!, event);
    }
    this.lastId = event.id;
  }

  ingest(input: UsageInput, receivedAt: string): { event: UsageEvent; duplicate: boolean } {
    const result = this.db.transaction(() => {
      if (input.request_id) {
        const row = this.db.prepare('SELECT * FROM usage_events WHERE request_id = ?').get(input.request_id) as Row | undefined;
        if (row) {
          const event = fromRow(row);
          if (event.agent_name !== input.agent_name || event.model !== input.model ||
              event.prompt_tokens !== input.prompt_tokens || event.completion_tokens !== input.completion_tokens) {
            throw new ConflictError('request_id ya existe con métricas diferentes');
          }
          return { event, duplicate: true };
        }
      }
      const event = {
        ...input, request_id: input.request_id ?? null, received_at: receivedAt,
        ...calculateCost(input, this.catalog, receivedAt),
      };
      const result = this.db.prepare(`INSERT INTO usage_events
        (request_id, received_at, agent_name, model, prompt_tokens, completion_tokens,
          input_cost_usd, output_cost_usd, cost_usd, pricing_json)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(
        event.request_id, event.received_at, event.agent_name, event.model,
        event.prompt_tokens, event.completion_tokens, event.input_cost_usd,
        event.output_cost_usd, event.cost_usd, event.pricing ? JSON.stringify(event.pricing) : null,
      );
      return { event: { ...event, id: Number(result.lastInsertRowid) }, duplicate: false };
    })();
    if (!result.duplicate) this.addToTotals(result.event);
    return result;
  }

  summary(): Summary {
    const groups = (map: Map<string, Accumulator>): Group[] => [...map].map(([name, value]) => ({ name, ...serialize(value) }))
      .sort((a, b) => new Decimal(b.cost_usd).comparedTo(a.cost_usd) || b.requests - a.requests || a.name.localeCompare(b.name));
    return { totals: serialize(this.totals), agents: groups(this.agents), models: groups(this.models) };
  }

  events(beforeId?: number, limit = 100): EventPage {
    const rows = beforeId === undefined
      ? this.db.prepare('SELECT * FROM usage_events ORDER BY id DESC LIMIT ?').all(limit + 1)
      : this.db.prepare('SELECT * FROM usage_events WHERE id < ? ORDER BY id DESC LIMIT ?').all(beforeId, limit + 1);
    const events = (rows as Row[]).slice(0, limit).map(fromRow);
    return { events, next_before_id: rows.length > limit ? events.at(-1)!.id : null };
  }

  snapshot(): Snapshot { return { ...this.summary(), ...this.events(), last_event_id: this.lastId }; }
  health() { this.db.prepare('SELECT 1').get(); }
  close() { this.db.close(); }
}
