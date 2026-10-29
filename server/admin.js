import { config } from './config.js';
import { openDb, issue, revoke } from './db.js';
process.umask(0o077);
const c = config(),
  db = openDb(c.db);
if (process.argv[2] === 'issue') console.log(JSON.stringify(issue(db), null, 2));
else if (process.argv[2] === 'revoke') {
  revoke(db);
  console.log('Todos los accesos revocados.');
} else {
  console.error('Uso: npm run access -- issue|revoke');
  process.exitCode = 1;
}
db.close();
