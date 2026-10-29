import { config } from './config.js';
import { openDb } from './db.js';
import { Sources } from './sources.js';
import { Tokenizer } from './tokenizer.js';
import { Collector } from './collector.js';
import { createApp } from './app.js';
process.umask(0o077);
const c = config(),
  db = openDb(c.db),
  collector = new Collector(db, c.codexRoot);
const tokenizer = new Tokenizer(db, c.tokenizerSocket),
  sources = new Sources(
    db,
    { openCodeDb: c.openCodeDb, geminiRoot: c.geminiRoot, antigravityRoot: c.antigravityRoot },
    tokenizer,
  );
const { app, broadcast, close } = createApp({ ...c, db, collector, sources, tokenizer });
collector.onChange = broadcast;
sources.onChange = broadcast;
tokenizer.onChange = broadcast;
const server = app.listen(c.backendPort, '127.0.0.1', () => {
  collector.start();
  sources.start();
  tokenizer.start();
  console.log('AI Usage Monitor backend ready');
});
process.on('SIGTERM', () => {
  collector.stop();
  sources.stop();
  tokenizer.stop();
  close();
  server.close(() => {
    db.close();
    process.exit(0);
  });
});
