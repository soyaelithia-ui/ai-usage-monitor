import unittest, sys, socket, tempfile, os
from unittest.mock import patch
sys.path.insert(0,os.path.dirname(__file__))
import tokenizer

class TokenizerTests(unittest.TestCase):
    def count(self, provider, model, text):
        with patch('requests.get',side_effect=AssertionError('network forbidden')):
            return tokenizer.count(dict(provider=provider,model=model,text=text))
    def test_unicode_and_versions(self):
        for provider,model in [('openai','gpt-4o'),('google','gemini-2.5-pro')]:
            r=self.count(provider,model,'¡Hola! 👋🏽 中文\nПривет e\u0301')
            self.assertEqual(r['status'],'estimated')
            self.assertGreater(int(r['tokens']),0)
            self.assertEqual(r,self.count(provider,model,'¡Hola! 👋🏽 中文\nПривет e\u0301'))
            self.assertEqual(r['coverage'],'visible_text_only')
            self.assertIn(r['version'],['0.14.0','2.34.0'])
    def test_no_assumed_astra_encoding(self):
        self.assertEqual(self.count('openai','gpt-6-astra','hello')['status'],'unsupported_model')
        self.assertEqual(self.count('google','gemini-4-argon','hello')['status'],'unsupported_model')
        self.assertEqual(self.count('anthropic','claude','hello')['status'],'unsupported_model')
    def test_text_limit(self):
        self.assertEqual(self.count('openai','gpt-4o','a'*(tokenizer.MAX_BYTES+1))['status'],'text_limit')
        self.assertEqual(self.count('google','gemini-2.5-pro',{'image':'x'})['status'],'text_limit')
    def test_special_tokens_are_plain_text(self):
        self.assertEqual(self.count('openai','gpt-4o','<|endoftext|>')['status'],'estimated')
    def test_empty_text_and_capabilities(self):
        self.assertEqual(self.count('openai','gpt-4o','')['tokens'],'0')
        c=tokenizer.count({'operation':'capabilities'})
        self.assertEqual(c['maxBytes'],2097152)
        self.assertIn('gemini-2.5-pro',c['googleModels'])
    def test_uncached_vocab_fails_closed_offline(self):
        tokenizer.ENGINES.clear()
        with patch('tiktoken.get_encoding',side_effect=OSError('offline')):
            self.assertEqual(self.count('openai','gpt-4o','hello')['status'],'vocabulary_unavailable')

if __name__=='__main__':unittest.main()
