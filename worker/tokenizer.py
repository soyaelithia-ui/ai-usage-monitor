"""Bounded, text-only tokenization. No agents, credentials or conversation files accessed.

Engines come from the declarative vocabulary catalogue (worker/vocabularies.json):
  - `tiktoken`      official OpenAI SDK mappings, cache-verified encodings.
  - `tiktoken-bpe`  official BPE rank files (ChatGLM, Kimi) with their own pattern.
  - `huggingface`   fast tokenizer.json files (Qwen, DeepSeek, GLM, Yi, ...).
  - `sentencepiece` SentencePiece models (Baichuan).
Every vocabulary file is checksum-verified before use; nothing is fetched at runtime.
"""
import fnmatch, hashlib, json, logging, multiprocessing, os, socket, sys
from collections import OrderedDict
from importlib.metadata import version
from pathlib import Path

import tiktoken

logging.disable(logging.CRITICAL)
MAX_BYTES = 2 * 1024 * 1024
ENGINE_CACHE = 4
ROOT = Path(__file__).resolve().parent
CATALOGUE = json.loads((ROOT / 'vocabularies.json').read_text())
VERSIONS = {'tiktoken': version('tiktoken')}
for _name in ('tokenizers', 'sentencepiece'):
    try:
        VERSIONS[_name] = version(_name)
    except Exception:  # noqa: BLE001 - engine simply not installed
        pass
ENGINES = OrderedDict()


def cache_root():
    return Path(
        os.environ.get(
            'USAGE_MONITOR_TOKENIZER_CACHE',
            os.environ.get(
                'PULSE_TOKENIZER_CACHE',
                os.path.join(
                    os.path.expanduser('~'), '.local', 'share', 'ai-usage-monitor', 'tokenizer-cache'
                ),
            ),
        )
    )


def vocabulary_path(key, entry):
    return cache_root() / 'vocabularies' / f'{key}__{entry["file"]}'


def verified_bytes(key, entry):
    path = vocabulary_path(key, entry)
    data = path.read_bytes()
    if hashlib.sha256(data).hexdigest() != entry['sha256']:
        raise ValueError('vocabulary checksum')
    return data


def match_vocabulary(model):
    """Catalogue order wins; a pattern without globs matches by prefix."""
    for key, entry in CATALOGUE.items():
        for pattern in entry.get('models', []):
            if any(c in pattern for c in '*?['):
                if fnmatch.fnmatchcase(model, pattern):
                    return key, entry
            elif model.startswith(pattern):
                return key, entry
    return None, None


def lru_put(key, engine):
    ENGINES[key] = engine
    ENGINES.move_to_end(key)
    while len(ENGINES) > ENGINE_CACHE:
        ENGINES.popitem(last=False)


def load_engine(key, entry):
    if key in ENGINES:
        ENGINES.move_to_end(key)
        return ENGINES[key]
    data = verified_bytes(key, entry)
    engine_name = entry['engine']
    if engine_name == 'huggingface':
        from tokenizers import Tokenizer

        engine = ('tokenizers', Tokenizer.from_file(str(vocabulary_path(key, entry))))
    elif engine_name == 'tiktoken-bpe':
        ranks = tiktoken.load.load_tiktoken_bpe(str(vocabulary_path(key, entry)))
        engine = (
            'tiktoken',
            tiktoken.Encoding(
                name=key, pat_str=entry['pattern'], mergeable_ranks=ranks, special_tokens={}
            ),
        )
    elif engine_name == 'sentencepiece':
        import sentencepiece as spm

        processor = spm.SentencePieceProcessor()
        if not processor.Load(str(vocabulary_path(key, entry))):
            raise ValueError('sentencepiece load failed')
        engine = ('sentencepiece', processor)
    else:
        raise ValueError('unknown engine')
    lru_put(key, engine)
    return engine


def count(request):
    if request.get('operation') == 'capabilities':
        return {
            'status': 'ready',
            'versions': VERSIONS,
            'openaiModels': [
                m
                for m, e in tiktoken.model.MODEL_TO_ENCODING.items()
                if e in ('o200k_base', 'cl100k_base', 'p50k_base', 'r50k_base', 'o200k_harmony')
            ],
            'openaiPrefixes': list(tiktoken.model.MODEL_PREFIX_TO_ENCODING),
            'vocabularies': [
                {
                    'id': key,
                    'models': entry.get('models', []),
                    'engine': entry['engine'],
                    'installed': vocabulary_path(key, entry).exists(),
                }
                for key, entry in CATALOGUE.items()
            ],
            'coverage': 'visible_text_only',
            'maxBytes': MAX_BYTES,
            'unsupported': ['gpt-6-astra', 'gpt-6.1-sol', 'gpt-6.1-terra'],
        }
    text = request.get('text')
    if not isinstance(text, str) or len(text.encode('utf-8')) > MAX_BYTES:
        return {'status': 'text_limit'}
    model, provider = request.get('model'), request.get('provider')
    if not isinstance(model, str) or len(model) > 160:
        return {'status': 'unsupported_model'}
    try:
        if provider == 'openai':
            encoding = tiktoken.encoding_name_for_model(model)  # Never invent a mapping.
            if encoding not in (
                'o200k_base',
                'cl100k_base',
                'p50k_base',
                'r50k_base',
                'o200k_harmony',
            ):
                return {'status': 'vocabulary_not_installed'}
            engine = ENGINES.get(encoding)
            if engine is None:
                lru_put(encoding, ('tiktoken', tiktoken.get_encoding(encoding)))
                engine = ENGINES[encoding]
            n = len(engine[1].encode(text, disallowed_special=()))
            name, vocabulary = 'tiktoken', encoding
        else:
            custom_key = request.get('vocabulary')
            if custom_key and custom_key in CATALOGUE:
                key, entry = custom_key, CATALOGUE[custom_key]
            else:
                key, entry = match_vocabulary(model.lower())
            if not entry:
                return {'status': 'unsupported_model'}
            engine_kind, engine = load_engine(key, entry)
            if engine_kind == 'tokenizers':
                n = len(engine.encode(text, add_special_tokens=False).ids)
            elif engine_kind == 'tiktoken':
                n = len(engine.encode(text, disallowed_special=()))
            else:
                n = len(engine.encode(text))
            name, vocabulary = engine_kind, key
        return {
            'status': 'estimated',
            'tokens': str(n),
            'method': 'local_text',
            'coverage': 'visible_text_only',
            'tokenizer': name,
            'version': VERSIONS[name],
            'vocabulary': vocabulary,
        }
    except KeyError:
        return {'status': 'unsupported_model'}
    except Exception:  # noqa: BLE001 - never log text or engine exceptions
        return {'status': 'vocabulary_unavailable'}


def prewarm():
    result = count({'provider': 'openai', 'model': 'gpt-4o', 'text': 'hello'})
    if result['status'] != 'estimated':
        raise RuntimeError('prewarm failed')
    missing = []
    for key, entry in CATALOGUE.items():
        try:
            verified_bytes(key, entry)
        except Exception:  # noqa: BLE001
            missing.append(key)
    if missing:
        raise RuntimeError(f'vocabulary missing or checksum mismatch: {", ".join(missing)}')
    print(
        'Verified tokenizer vocabularies cached:'
        f" {len(CATALOGUE) - len(missing)}/{len(CATALOGUE)} vocabularies"
    )


def serve(path):
    os.umask(0o077)
    if os.path.exists(path):
        os.unlink(path)
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as server:
        server.bind(path)
        os.chmod(path, 0o600)
        server.listen(4)
        while True:
            conn, _ = server.accept()
            with conn:
                conn.settimeout(3)
                try:
                    data = bytearray()
                    while b'\n' not in data and len(data) <= MAX_BYTES + 4096:
                        chunk = conn.recv(65536)
                        if not chunk:
                            break
                        data.extend(chunk)
                    if len(data) > MAX_BYTES + 4096:
                        result = {'status': 'text_limit'}
                    else:
                        request = json.loads(data)
                        if request.get('operation') == 'capabilities':
                            result = count(request)
                        else:
                            # Kill a slow tokenizer after 8 s without blocking collection or agents.
                            def job():
                                try:
                                    conn.sendall(json.dumps(count(request)).encode() + b'\n')
                                except Exception:  # noqa: BLE001
                                    pass

                            process = multiprocessing.get_context('fork').Process(target=job)
                            process.start()
                            process.join(8)
                            if process.is_alive():
                                process.kill()
                                process.join()
                                result = {'status': 'timeout'}
                            elif process.exitcode != 0:
                                result = {'status': 'worker_error'}
                            else:
                                continue
                    conn.sendall(json.dumps(result).encode() + b'\n')
                except Exception:  # noqa: BLE001
                    try:
                        conn.sendall(b'{"status":"worker_error"}\n')
                    except OSError:
                        pass


if __name__ == '__main__':
    if '--prewarm' in sys.argv:
        prewarm()
    elif '--stdio' in sys.argv:
        for line in sys.stdin:
            print(json.dumps(count(json.loads(line))), flush=True)
    else:
        serve(
            os.environ.get(
                'USAGE_MONITOR_TOKENIZER_SOCKET',
                os.environ.get(
                    'PULSE_TOKENIZER_SOCKET',
                    '/var/lib/ai-usage-monitor/tokenizer.sock',
                ),
            )
        )
