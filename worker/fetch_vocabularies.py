"""Download every declared vocabulary at its pinned revision and verify its checksum.

Present files with a matching checksum are kept. Nothing is fetched at runtime by
the worker itself; this script is the single network-touching installation step.
"""
import hashlib
import json
import os
import sys
import urllib.request
from pathlib import Path

CATALOGUE = json.loads((Path(__file__).resolve().parent / 'vocabularies.json').read_text())


def cache_root():
    return Path(
        os.environ.get(
            'USAGE_MONITOR_TOKENIZER_CACHE',
            os.path.join(
                os.path.expanduser('~'), '.local', 'share', 'ai-usage-monitor', 'tokenizer-cache'
            ),
        )
    )


def main():
    root = cache_root() / 'vocabularies'
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    for key, entry in CATALOGUE.items():
        destination = root / f"{key}__{entry['file']}"
        if destination.exists() and (
            hashlib.sha256(destination.read_bytes()).hexdigest() == entry['sha256']
        ):
            continue
        url = f"https://huggingface.co/{entry['repo']}/resolve/{entry['revision']}/{entry['file']}"
        request = urllib.request.Request(url, headers={'User-Agent': 'usage-monitor-catalog/1.0'})
        digest = hashlib.sha256()
        temporary = destination.with_name(destination.name + '.partial')
        try:
            with urllib.request.urlopen(request, timeout=600) as response, open(
                temporary, 'wb'
            ) as handle:
                while True:
                    chunk = response.read(1 << 20)
                    if not chunk:
                        break
                    digest.update(chunk)
                    handle.write(chunk)
        except Exception as error:  # noqa: BLE001
            temporary.unlink(missing_ok=True)
            sys.exit(f'Download failed for {key}: {error}')
        if digest.hexdigest() != entry['sha256']:
            temporary.unlink(missing_ok=True)
            sys.exit(f'Checksum mismatch for {key}; vocabulary not installed')
        os.chmod(temporary, 0o600)
        temporary.replace(destination)
        print(f'Installed {key}')
    print(f'Verified {len(CATALOGUE)} vocabularies in {root}')


if __name__ == '__main__':
    main()
