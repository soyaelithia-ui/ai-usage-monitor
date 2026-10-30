#!/bin/bash
set -euo pipefail
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"
umask 077
data_dir=${USAGE_MONITOR_DATA_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/ai-usage-monitor}
python_bin=${PYTHON:-python3}
mkdir -p "$data_dir/tokenizer-cache" "$data_dir/backups"
"$python_bin" -m venv "$data_dir/tokenizer-venv"
if command -v uv >/dev/null 2>&1; then
  uv pip sync --python "$data_dir/tokenizer-venv/bin/python" --require-hashes worker/requirements.lock
else
  "$data_dir/tokenizer-venv/bin/python" -m pip install --require-hashes -r worker/requirements.lock
fi
# Download every declared vocabulary at its pinned revision and verify its checksum.
# Present files with a matching checksum are kept; nothing is fetched at runtime.
USAGE_MONITOR_TOKENIZER_CACHE="$data_dir/tokenizer-cache" \
  "$data_dir/tokenizer-venv/bin/python" - <<'PY'
import hashlib, json, os, sys, urllib.request
from pathlib import Path

catalogue = json.loads(Path('worker/vocabularies.json').read_text())
root = Path(os.environ['USAGE_MONITOR_TOKENIZER_CACHE']) / 'vocabularies'
root.mkdir(mode=0o700, parents=True, exist_ok=True)
for key, entry in catalogue.items():
    dest = root / f"{key}__{entry['file']}"
    if dest.exists() and hashlib.sha256(dest.read_bytes()).hexdigest() == entry['sha256']:
        continue
    url = f"https://huggingface.co/{entry['repo']}/resolve/{entry['revision']}/{entry['file']}"
    request = urllib.request.Request(url, headers={'User-Agent': 'usage-monitor-catalog/1.0'})
    digest = hashlib.sha256()
    temporary = dest.with_name(dest.name + '.partial')
    try:
        with urllib.request.urlopen(request, timeout=600) as response, open(temporary, 'wb') as handle:
            while True:
                chunk = response.read(1 << 20)
                if not chunk:
                    break
                digest.update(chunk)
                handle.write(chunk)
    except Exception as error:  # noqa: BLE001
        temporary.unlink(missing_ok=True)
        sys.exit(f"Download failed for {key}: {error}")
    if digest.hexdigest() != entry['sha256']:
        temporary.unlink(missing_ok=True)
        sys.exit(f"Checksum mismatch for {key}; vocabulary not installed")
    os.chmod(temporary, 0o600)
    temporary.replace(dest)
    print(f"Installed {key}")
PY
TMPDIR="$data_dir/tokenizer-cache" TIKTOKEN_CACHE_DIR="$data_dir/tokenizer-cache/tiktoken" USAGE_MONITOR_TOKENIZER_CACHE="$data_dir/tokenizer-cache" "$data_dir/tokenizer-venv/bin/python" worker/tokenizer.py --prewarm
