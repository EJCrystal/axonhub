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
  assert.match(settings, /isSavedRow\(index\)/, 'the button checks the row against the saved config');
  assert.match(settings, /!canRun \|\| readOnly \|\| runPending \|\| !saved/, 'and is disabled otherwise');
});

// The old header menu is gone; the button that ran everything stays.
test('the header no longer offers a per-channel menu', () => {
  assert.doesNotMatch(page, /DropdownMenu/, 'the menu was removed');
  assert.match(page, /onClick=\{\(\) => runNow\.mutate\(undefined\)\}/, 'the primary button still runs everything');
});

// The three history actions share one toolbar. Settings has no history action
// cluster, so the controls disappear when the settings tab is selected.
test('the history toolbar carries the three related actions', () => {
  const toolbar = page.slice(page.indexOf("<div className='bg-card/70"), page.indexOf("<TabsContent value='history'"));
  assert.match(toolbar, /intelligence\.history\.refresh/, 'the toolbar has refresh');
  assert.match(toolbar, /\{filterPopover\}/, 'the toolbar has filters');
  assert.match(toolbar, /\{runButton\}/, 'the toolbar has the run action');
  assert.match(toolbar, /activeTab === 'history'/, 'the history actions are scoped to history');
  assert.doesNotMatch(page, /actions=\{activeTab === 'settings'/, 'the page header no longer owns the tab actions');
});

// A bare "run now" reads as if it covered whatever is in view, so the label
// names the scope and the count.
test('the run button names its scope and count', () => {
  assert.match(page, /intelligence\.runAllTargets/, 'the label states it covers every configured target');
  assert.match(page, /intelligence\.runAllTargetsHint/, 'and explains what that does');
  assert.match(page, /count: configuredTargets\.length/, 'the count comes from the configuration');
});

test('the mutation forwards the key to the backend', () => {
  assert.match(data, /\$apiKey: String/, 'the mutation declares the variable');
  assert.match(data, /runIntelligenceCheckNow\(channelID: \$channelID, apiKey: \$apiKey\)/, 'and passes it');
});
