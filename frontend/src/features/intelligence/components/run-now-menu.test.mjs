import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

// No DOM here, so the run-now menu is pinned by reading the page. A channel can
// be configured once per key, so an entry that names only the channel would run
// every key on it and re-bill the ones the reader did not ask for.
const dir = import.meta.dirname;
const page = readFileSync(join(dir, '..', 'index.tsx'), 'utf8');
const data = readFileSync(join(dir, '..', 'data', 'intelligence.ts'), 'utf8');

test('the menu passes the key as well as the channel', () => {
  assert.match(
    page,
    /runNow\.mutate\(\{ channelID: target\.channelID, apiKey: target\.apiKey \}\)/,
    'the entry must identify one target, not one channel'
  );
});

test('each entry shows the key, model and level so duplicates are told apart', () => {
  assert.match(page, /keyLabel: target\.apiKey \? maskAPIKey\(target\.apiKey\)/, 'a masked key per entry');
  assert.match(page, /\{target\.keyLabel &&/, 'the key is rendered');
  assert.match(page, /\{target\.modelID\}/, 'the model is rendered');
  assert.match(page, /\{target\.reasoningEffort &&/, 'the level is rendered when set');
});

test('the mutation forwards the key to the backend', () => {
  assert.match(data, /\$apiKey: String/, 'the mutation declares the variable');
  assert.match(data, /runIntelligenceCheckNow\(channelID: \$channelID, apiKey: \$apiKey\)/, 'and passes it');
});

// The plain button keeps running everything, which is the documented default.
test('the primary button still runs every configured target', () => {
  assert.match(page, /onClick=\{\(\) => runNow\.mutate\(undefined\)\}/);
});
