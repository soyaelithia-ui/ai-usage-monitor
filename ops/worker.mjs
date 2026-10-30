import { spawn } from 'node:child_process';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { config } from '../server/config.js';
const c = config(),
  root = dirname(dirname(fileURLToPath(import.meta.url))),
  cache = join(c.dataDir, 'tokenizer-cache');
const child = spawn(
  join(c.dataDir, 'tokenizer-venv', 'bin', 'python'),
  [join(root, 'worker', 'tokenizer.py')],
  {
    stdio: 'inherit',
    env: {
      PATH: process.env.PATH ?? '/usr/bin:/bin',
      LANG: 'C.UTF-8',
      TMPDIR: cache,
      TIKTOKEN_CACHE_DIR: join(cache, 'tiktoken'),
      USAGE_MONITOR_TOKENIZER_CACHE: cache,
      USAGE_MONITOR_TOKENIZER_SOCKET: c.tokenizerSocket,
      PYTHONWARNINGS: 'ignore',
    },
  },
);
child.on('error', () => {
  console.error('Tokenizer unavailable; run bash ops/setup-tokenizers.sh');
  process.exitCode = 1;
});
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal));
child.on('exit', (code) => {
  process.exitCode = code ?? 1;
});
