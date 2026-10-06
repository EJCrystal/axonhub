import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

const dataDir = import.meta.dirname;
const source = readFileSync(join(dataDir, 'channel-id.ts'), 'utf8');
const transpiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
}).outputText;
const moduleUrl = `data:text/javascript;base64,${Buffer.from(transpiled).toString('base64')}`;
const { channelIdKey, sameChannelId } = await import(moduleUrl);

test('the numeric id is what identifies a channel', () => {
  assert.equal(channelIdKey('gid://axonhub/Channel/6'), '6');
  assert.equal(channelIdKey('gid://axonhub/channel/6'), '6');
});

// The bug this guards: the API returns gid://axonhub/Channel/6 while a saved
// configuration used gid://axonhub/channel/6. Comparing the whole string left
// the settings row unable to find its channel, so the pickers rendered empty.
test('the capitalized and lowercase type segments are the same channel', () => {
  assert.ok(sameChannelId('gid://axonhub/Channel/6', 'gid://axonhub/channel/6'));
  assert.ok(sameChannelId('gid://axonhub/channel/6', 'gid://axonhub/Channel/6'));
});

test('different channels stay different', () => {
  assert.ok(!sameChannelId('gid://axonhub/Channel/6', 'gid://axonhub/Channel/7'));
  assert.ok(!sameChannelId('gid://axonhub/channel/1', 'gid://axonhub/Channel/11'));
});

test('a bare id compares equal to its own gid', () => {
  assert.ok(sameChannelId('6', 'gid://axonhub/Channel/6'));
});
