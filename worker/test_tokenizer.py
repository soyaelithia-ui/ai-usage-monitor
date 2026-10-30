import os, sys, unittest
from unittest.mock import patch

sys.path.insert(0, os.path.dirname(__file__))
import tokenizer


class TokenizerTests(unittest.TestCase):
    def count(self, provider, model, text):
        # No network access is ever allowed while counting.
        with patch('urllib.request.urlopen', side_effect=AssertionError('network forbidden')):
            return tokenizer.count(dict(provider=provider, model=model, text=text))

    def test_every_engine_estimates_and_is_deterministic(self):
        cases = [
            ('openai', 'gpt-4o', 'tiktoken'),
            ('zai', 'glm-4.5', 'tokenizers'),
            ('deepseek', 'deepseek-v3', 'tokenizers'),
            ('moonshot', 'kimi-k2-instruct', 'tiktoken'),
            ('zhipu', 'chatglm4-9b', 'tiktoken'),
            ('baichuan', 'baichuan2-13b-chat', 'sentencepiece'),
            ('alibaba', 'qwen3-235b-a22b', 'tokenizers'),
        ]
        text = '¡Hola! 👋🏽 中文\nПривет e\u0301'
        for provider, model, engine in cases:
            first = self.count(provider, model, text)
            self.assertEqual(first['status'], 'estimated', first)
            self.assertGreater(int(first['tokens']), 0)
            self.assertEqual(first['tokenizer'], engine)
            self.assertEqual(first, self.count(provider, model, text))
            self.assertEqual(first['coverage'], 'visible_text_only')

    def test_no_assumed_encoding_for_unmapped_models(self):
        self.assertEqual(self.count('openai', 'gpt-6-astra', 'hello')['status'], 'unsupported_model')
        self.assertEqual(self.count('anthropic', 'claude', 'hello')['status'], 'unsupported_model')
        self.assertEqual(self.count('other', 'totally-unknown', 'hello')['status'], 'unsupported_model')

    def test_text_limit(self):
        self.assertEqual(
            self.count('openai', 'gpt-4o', 'a' * (tokenizer.MAX_BYTES + 1))['status'], 'text_limit'
        )
        self.assertEqual(self.count('openai', 'gpt-4o', {'image': 'x'})['status'], 'text_limit')

    def test_special_tokens_are_plain_text(self):
        self.assertEqual(self.count('openai', 'gpt-4o', '')['tokens'], '0')

    def test_capabilities_list_vocabularies(self):
        capabilities = tokenizer.count({'operation': 'capabilities'})
        self.assertEqual(capabilities['maxBytes'], 2097152)
        self.assertGreaterEqual(len(capabilities['vocabularies']), 13)
        self.assertIn('openaiModels', capabilities)
        self.assertTrue(
            all('installed' in vocabulary for vocabulary in capabilities['vocabularies'])
        )

    def test_checksum_mismatch_fails_closed(self):
        tokenizer.ENGINES.clear()
        with patch.object(tokenizer, 'verified_bytes', side_effect=ValueError('vocabulary checksum')):
            result = self.count('alibaba', 'qwen3-8b', 'hello')
        self.assertEqual(result['status'], 'vocabulary_unavailable')


if __name__ == '__main__':
    unittest.main()
