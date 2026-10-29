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
TMPDIR="$data_dir/tokenizer-cache" TIKTOKEN_CACHE_DIR="$data_dir/tokenizer-cache/tiktoken" USAGE_MONITOR_TOKENIZER_CACHE="$data_dir/tokenizer-cache" "$data_dir/tokenizer-venv/bin/python" worker/tokenizer.py --prewarm
