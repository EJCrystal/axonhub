import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

// Load the real module by transpiling it, so the assertions cannot drift from
// the implementation the settings screen imports.
const dataDir = import.meta.dirname;
const source = readFileSync(join(dataDir, 'api-key-models.ts'), 'utf8');
const transpiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
}).outputText;
const moduleUrl = `data:text/javascript;base64,${Buffer.from(transpiled).toString('base64')}`;
const { modelsForAPIKey } = await import(moduleUrl);

test('an explicit per-key assignment wins', () => {
  const models = modelsForAPIKey(
    {
      apiKeys: ['k1'],
      apiKeyModels: [{ apiKey: 'k1', models: ['only-this'] }],
      apiKeyFetchedModels: [{ apiKey: 'k1', models: ['a', 'b'] }],
    },
    ['a', 'b'],
    'k1'
  );
  assert.deepEqual(models, ['only-this']);
});

test('an explicit empty assignment means the key serves nothing', () => {
  const models = modelsForAPIKey({ apiKeys: ['k1'], apiKeyModels: [{ apiKey: 'k1', models: [] }] }, ['a'], 'k1');
  assert.deepEqual(models, []);
});

test('without an explicit entry the key is limited to its fetched capability', () => {
  const models = modelsForAPIKey({ apiKeys: ['k1'], apiKeyFetchedModels: [{ apiKey: 'k1', models: ['a'] }] }, ['a', 'b'], 'k1');
  assert.deepEqual(models, ['a']);
});

test('a key with no fetched data falls back to the channel list', () => {
  const models = modelsForAPIKey({ apiKeys: ['k1'] }, ['a', 'b'], 'k1');
  assert.deepEqual(models, ['a', 'b']);
});

test('duplicate channel models are collapsed', () => {
  const models = modelsForAPIKey(null, ['a', 'a', 'b'], 'k1');
  assert.deepEqual(models, ['a', 'b']);
});
