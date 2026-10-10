import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

// There is no DOM in this suite, so the target-row rules are pinned by reading
// the component. The point of these rows: one channel may carry several keys,
// each with its own model and thinking level, but a key must not repeat.
const source = readFileSync(join(import.meta.dirname, 'intelligence-settings.tsx'), 'utf8');

const pairGate = source.slice(source.indexOf('const isPairConfigured'), source.indexOf('const addTarget'));

test('a row is identified by channel plus key, not by channel alone', () => {
  assert.ok(pairGate, 'the pair gate must exist');
  assert.match(pairGate, /sameChannelId\(target\.channelID, channelID\)/, 'channel comparison');
  assert.match(pairGate, /target\.apiKey === apiKey/, 'key comparison');
  assert.doesNotMatch(pairGate, /seen\.has\(channelIdKey/, 'a channel-only uniqueness check would forbid several keys per channel');
});

// Switching a row to another channel must land on a key that channel has not
// already used, otherwise the save would be rejected as a duplicate.
test('switching channel picks a key that is still free there', () => {
  assert.match(
    source,
    /const apiKey = keysFor\(value\)\.find\(\(key\) => !isPairConfigured\(value, key, index\)\)/,
    'the new channel must resolve to a free key'
  );
});

// The add button has to know whether anything is left to add.
test('the add button is disabled once every pair is configured', () => {
  assert.match(source, /disabled=\{readOnly \|\| loading \|\| !firstFreePair\(\)\}/);
});

// The channel picker must list every channel, since a channel is now allowed to
// appear again for a different key.
test('the channel picker no longer filters out used channels', () => {
  const select = source.slice(
    source.indexOf('{channelOptions.map((channel) => ('),
    source.indexOf('</SelectContent>', source.indexOf('{channelOptions.map((channel) => ('))
  );
  assert.ok(select, 'the channel picker must exist');
  assert.doesNotMatch(select, /used\.has\(/, 'channels must not be filtered out');
});

// A row keeps its own key selectable even when the pair would read as taken.
test('a row can still render its own already-chosen key', () => {
  assert.match(source, /if \(target\.apiKey && !keys\.includes\(target\.apiKey\)\) keys\.unshift\(target\.apiKey\)/);
});

// Adding a target collects its channel, key, model and check first. Inserting a
// row straight into the table started every new target on the first free pair
// and left the reader to correct each field by hand.
const settingsSource = readFileSync(join(import.meta.dirname, 'intelligence-settings.tsx'), 'utf8');
const dialogSource = readFileSync(join(import.meta.dirname, 'intelligence-add-target-dialog.tsx'), 'utf8');

test('adding a target opens a dialog instead of writing a row', () => {
  assert.match(settingsSource, /onClick=\{\(\) => setAddOpen\(true\)\}/, 'the button opens the dialog');
  assert.match(settingsSource, /<IntelligenceAddTargetDialog/, 'the dialog is mounted');
  assert.doesNotMatch(settingsSource, /const pair = firstFreePair\(\)/, 'no row is written without asking');
});

test('the dialog asks for every field the row needs', () => {
  for (const field of ['add-target-channel', 'add-target-key', 'add-target-model', 'add-target-effort', 'add-target-benchmark']) {
    assert.match(dialogSource, new RegExp(field), `the dialog asks for ${field}`);
  }
  assert.match(dialogSource, /add-target-confirm/, 'and confirms with one button');
});

// A channel is configured once per key, so the dialog must not offer a pair the
// table already holds.
test('the dialog only offers keys that are still free', () => {
  assert.match(dialogSource, /!isPairConfigured\(channel\.id, key\)/, 'taken pairs are filtered out');
  assert.match(dialogSource, /disabled=\{!canAdd\}/, 'and a taken pair cannot be confirmed');
});

// The channel list is longer than a dropdown can usefully show, so the picker is
// searchable: typing part of a name narrows it. Radix Select could not do this.
test('the channel picker is searchable', () => {
  assert.match(dialogSource, /CommandInput/, 'the picker has a search box');
  assert.match(dialogSource, /addDialog\.channelSearch/, 'with a placeholder');
  assert.match(dialogSource, /CommandEmpty/, 'and an empty state for no match');
  assert.match(dialogSource, /add-target-channel/, 'wired to the channel control');
});

test('the searchable picker keeps the key and model in step', () => {
  // Choosing a channel must reselect its first free key and that key's model,
  // otherwise the row would submit a key that belongs to another channel.
  const onSelect = dialogSource.slice(dialogSource.indexOf('onSelect={() => {'), dialogSource.indexOf('setChannelPickerOpen(false)'));
  assert.match(onSelect, /setApiKey/, 'the key follows the channel');
  assert.match(onSelect, /setModelID/, 'and so does the model');
});

// The settings screen holds two different jobs, so it is two cards: deciding
// when the check runs, and deciding what it runs against. One card buried the
// schedule under a table that can be long.
test('the settings screen splits the schedule from the targets', () => {
  assert.match(settingsSource, /intelligence\.settings\.scheduleTitle/, 'the schedule card is titled');
  assert.match(settingsSource, /intelligence\.settings\.targetsTitle/, 'and so is the targets card');

  const cards = settingsSource.match(/<Card>/g) ?? [];
  assert.equal(cards.length, 2, 'exactly two cards');

  // The switch and the interval belong to the first card; the table to the second.
  const firstCard = settingsSource.slice(0, settingsSource.indexOf('intelligence.settings.targetsTitle'));
  assert.match(firstCard, /intelligence-enabled/, 'the schedule card holds the switch');
  assert.match(firstCard, /intelligence-interval/, 'and the interval');
  assert.doesNotMatch(firstCard, /<Table>/, 'the table is not in the schedule card');
});
