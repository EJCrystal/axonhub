import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

// No DOM here, so the per-target run control is pinned by reading the sources.
// A channel can be configured once per key, so the run has to name the key as
// well as the channel, or it would re-check every key on it.
const dir = import.meta.dirname;
const page = readFileSync(join(dir, '..', 'index.tsx'), 'utf8');
const settings = readFileSync(join(dir, 'intelligence-settings.tsx'), 'utf8');
const data = readFileSync(join(dir, '..', 'data', 'intelligence.ts'), 'utf8');

// The run control lives next to the row that configures the target, rather than
// in a menu in the page header.
test('the run button sits on the settings row and names the key', () => {
  assert.match(
    settings,
    /runNow\.mutate\(\{ channelID: target\.channelID, apiKey: target\.apiKey \}\)/,
    'the row must identify one target, not one channel'
  );
  assert.match(settings, /intelligence\.settings\.runThisTarget/, 'the button is labelled');
});

// Running reads the stored configuration, so a row that was edited but not saved
// cannot be run: the click would silently check the previous values.
test('only a saved row can be run', () => {
  assert.match(settings, /isSavedTarget\(index\)/, 'the button checks the row against the saved config');
  assert.match(settings, /!canRun \|\| readOnly \|\| runNow\.isPending \|\| !isSavedTarget\(index\)/, 'and is disabled otherwise');
});

// The old header menu is gone; the button that ran everything stays.
test('the header no longer offers a per-channel menu', () => {
  assert.doesNotMatch(page, /DropdownMenu/, 'the menu was removed');
  assert.match(page, /onClick=\{\(\) => runNow\.mutate\(undefined\)\}/, 'the primary button still runs everything');
});

test('the mutation forwards the key to the backend', () => {
  assert.match(data, /\$apiKey: String/, 'the mutation declares the variable');
  assert.match(data, /runIntelligenceCheckNow\(channelID: \$channelID, apiKey: \$apiKey\)/, 'and passes it');
});
