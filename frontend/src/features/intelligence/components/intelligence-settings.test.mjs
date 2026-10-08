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
