import { config } from './config.js';
import { agents } from './catalog.js';
import { openDb } from './db.js';
import { Sources } from './sources.js';
import { Tokenizer } from './tokenizer.js';
import { Collector } from './collector.js';
import { createApp } from './app.js';
process.umask(0o077);
const c = config(),
  db = openDb(c.db);
// One declarative catalogue drives every reader: a JSONL collector per
// `jsonl` agent, and Sources for the sqlite/contract readers.
const collectors = agents
  .filter((a) => a.reader === 'jsonl')
  .map((a) => new Collector(db, c[a.configKey], a));
const paths = Object.fromEntries(
  agents.filter((a) => a.configKey).map((a) => [a.configKey, c[a.configKey]]),
);
const tokenizer = new Tokenizer(db, c.tokenizerSocket),
  sources = new Sources(db, paths, tokenizer);
const { app, broadcast, close } = createApp({ ...c, db, collectors, sources, tokenizer });
for (const collector of collectors) collector.onChange = broadcast;
sources.onChange = broadcast;
tokenizer.onChange = broadcast;
const server = app.listen(c.backendPort, '127.0.0.1', () => {
  for (const collector of collectors) collector.start();
  sources.start();
  tokenizer.start();
  console.log('AI Usage Monitor backend ready');
});
process.on('SIGTERM', () => {
  for (const collector of collectors) collector.stop();
  sources.stop();
  tokenizer.stop();
  close();
  server.close(() => {
    db.close();
    process.exit(0);
  });
});
