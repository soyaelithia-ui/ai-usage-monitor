"""Bounded, text-only tokenization. No agents, credentials or conversation files accessed."""
import json, os, socket, sys, logging, hashlib, multiprocessing
from importlib.metadata import version
import tiktoken
from google.genai.local_tokenizer import LocalTokenizer
from google.genai import _local_tokenizer_loader as loader

logging.disable(logging.CRITICAL)
MAX_BYTES = 2 * 1024 * 1024
# Only official SDK mappings whose vocabulary can be cached and hash-verified.
GOOGLE_MODELS = {**loader._GEMINI_MODELS_TO_TOKENIZER_NAMES,
                 **loader._GEMINI_STABLE_MODELS_TO_TOKENIZER_NAMES}
ENGINES = {}
VERSIONS = {'tiktoken': version('tiktoken'), 'google-genai': version('google-genai')}

# SDK normally uses tempfile (which falls back when TMPDIR is read-only).
# Runtime only reads prewarmed, checksum-verified vocabularies from our fixed cache.
if '--prewarm' not in sys.argv:
    def cached_google_vocabulary(*, file_url, expected_hash):
        root=os.environ.get('USAGE_MONITOR_TOKENIZER_CACHE',os.path.join(os.path.expanduser('~'),'.local','share','ai-usage-monitor','tokenizer-cache'))
        path=os.path.join(root,'vertexai_tokenizer_model',hashlib.sha1(file_url.encode()).hexdigest())
        with open(path,'rb') as f: data=f.read()
        if hashlib.sha256(data).hexdigest()!=expected_hash: raise ValueError('vocabulary checksum')
        return data
    loader._load=cached_google_vocabulary

def count(request):
    if request.get('operation') == 'capabilities':
        return {'status':'ready', 'versions':VERSIONS, 'googleModels':list(GOOGLE_MODELS),
                'openaiModels':[m for m,e in tiktoken.model.MODEL_TO_ENCODING.items() if e in ('o200k_base','cl100k_base','p50k_base','r50k_base','o200k_harmony')],
                'openaiPrefixes':list(tiktoken.model.MODEL_PREFIX_TO_ENCODING),
                'coverage':'visible_text_only', 'maxBytes':MAX_BYTES,
                'unsupported':['gpt-6-astra','gpt-6.1-sol','gpt-6.1-terra'],
                'googleUnavailable':list(loader._GEMINI_MODELS_TO_HUGGINGFACE_TOKENIZER_NAMES)}
    text = request.get('text')
    if not isinstance(text,str) or len(text.encode('utf-8')) > MAX_BYTES:
        return {'status':'text_limit'}
    model, provider = request.get('model'), request.get('provider')
    if not isinstance(model,str) or len(model)>160: return {'status':'unsupported_model'}
    try:
        if provider == 'openai':
            encoding = tiktoken.encoding_name_for_model(model)  # Never invent a mapping.
            if encoding not in ('o200k_base','cl100k_base','p50k_base','r50k_base','o200k_harmony'):
                return {'status':'vocabulary_not_installed'}
            engine = ENGINES.setdefault(encoding, tiktoken.get_encoding(encoding))
            n = len(engine.encode(text, disallowed_special=()))
            name = 'tiktoken'; vocabulary = encoding
        elif provider == 'google' and model in GOOGLE_MODELS:
            if model not in ENGINES: ENGINES[model] = LocalTokenizer(model_name=model)
            n = ENGINES[model].count_tokens(text).total_tokens
            name = 'google-genai'; vocabulary = GOOGLE_MODELS[model]
        else: return {'status':'unsupported_model'}
        return {'status':'estimated', 'tokens':str(n),'method':'local_text',
                'coverage':'visible_text_only', 'tokenizer':name,
                'version':VERSIONS[name], 'vocabulary':vocabulary}
    except KeyError: return {'status':'unsupported_model'}
    except Exception: return {'status':'vocabulary_unavailable'}  # Never log text or SDK exceptions.

def prewarm():
    for model in ['gpt-4o','gpt-4','text-davinci-003','davinci','gpt-oss-20b']:
        result=count({'provider':'openai','model':model,'text':'hello'})
        if result['status']!='estimated': raise RuntimeError('prewarm failed')
    result=count({'provider':'google','model':'gemini-2.5-pro','text':'hello'})
    if result['status']!='estimated': raise RuntimeError('prewarm failed')
    print('Verified tokenizer vocabularies cached')

def serve(path):
    os.umask(0o077)
    if os.path.exists(path): os.unlink(path)
    with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as server:
        server.bind(path); os.chmod(path,0o600); server.listen(4)
        while True:
            conn,_ = server.accept()
            with conn:
                conn.settimeout(3)
                try:
                    data=bytearray()
                    while b'\n' not in data and len(data)<=MAX_BYTES+4096:
                        chunk=conn.recv(65536)
                        if not chunk: break
                        data.extend(chunk)
                    if len(data)>MAX_BYTES+4096: result={'status':'text_limit'}
                    else:
                        request=json.loads(data)
                        if request.get('operation')=='capabilities': result=count(request)
                        else:
                            # Kill a slow tokenizer after 8 s without blocking collection or agents.
                            def job():
                                try: conn.sendall(json.dumps(count(request)).encode()+b'\n')
                                except Exception: pass
                            process=multiprocessing.get_context('fork').Process(target=job)
                            process.start(); process.join(8)
                            if process.is_alive():
                                process.kill(); process.join(); result={'status':'timeout'}
                            elif process.exitcode!=0: result={'status':'worker_error'}
                            else: continue
                    conn.sendall(json.dumps(result).encode()+b'\n')
                except Exception:
                    try: conn.sendall(b'{"status":"worker_error"}\n')
                    except OSError: pass

if __name__=='__main__':
    if '--prewarm' in sys.argv: prewarm()
    elif '--stdio' in sys.argv:
        for line in sys.stdin: print(json.dumps(count(json.loads(line))),flush=True)
    else: serve(os.environ.get('USAGE_MONITOR_TOKENIZER_SOCKET','/var/lib/ai-usage-monitor/tokenizer.sock'))
