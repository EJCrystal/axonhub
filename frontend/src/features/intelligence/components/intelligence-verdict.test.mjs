import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

// Load the real module by transpiling it, so the assertions cannot drift from
// the implementation the history table imports.
const componentDir = import.meta.dirname;
const source = readFileSync(join(componentDir, 'intelligence-verdict.ts'), 'utf8');
const transpiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
}).outputText;
const moduleUrl = `data:text/javascript;base64,${Buffer.from(transpiled).toString('base64')}`;
const { intelligenceVerdict, runIntelligenceVerdict, verdictBadgeVariant } = await import(moduleUrl);

test('a normal assessment maps to normal', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'normal' }), 'normal');
});

test('degraded and the historical suspicious label both map to degraded', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'degraded' }), 'degraded');
  assert.equal(intelligenceVerdict({ success: true, quality: 'suspicious' }), 'degraded');
});

test('an unrecognized quality is inconclusive, not degraded', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'unknown' }), 'unknown');
  assert.equal(intelligenceVerdict({ success: true, quality: '' }), 'unknown');
});

test('a failed key has no verdict', () => {
  assert.equal(intelligenceVerdict({ success: false, quality: 'degraded' }), 'incomplete');
});

// The bug this guards: the run status says "succeeded" whenever every key
// produced an answer, so degraded results used to be advertised as a pass.
test('a run whose keys all came back degraded reads as degraded, not a pass', () => {
  assert.equal(
    runIntelligenceVerdict({
      successKeys: 1,
      results: [{ success: true, quality: 'degraded' }],
    }),
    'degraded'
  );
});

test('a run is only normal when every key is normal', () => {
  assert.equal(
    runIntelligenceVerdict({
      successKeys: 2,
      results: [
        { success: true, quality: 'normal' },
        { success: true, quality: 'normal' },
      ],
    }),
    'normal'
  );

  assert.equal(
    runIntelligenceVerdict({
      successKeys: 2,
      results: [
        { success: true, quality: 'normal' },
        { success: true, quality: 'degraded' },
      ],
    }),
    'degraded',
    'one degraded key decides the run'
  );
});

test('a run with no successful key is incomplete', () => {
  assert.equal(
    runIntelligenceVerdict({ successKeys: 0, results: [{ success: false, quality: '' }] }),
    'incomplete'
  );
  assert.equal(runIntelligenceVerdict({ successKeys: 0, results: [] }), 'incomplete');
});

test('a run of inconclusive keys is unknown', () => {
  assert.equal(
    runIntelligenceVerdict({
      successKeys: 1,
      results: [{ success: true, quality: 'unknown' }],
    }),
    'unknown'
  );
});

test('degraded is the alarming badge and normal is the positive one', () => {
  assert.equal(verdictBadgeVariant('degraded'), 'destructive');
  assert.equal(verdictBadgeVariant('normal'), 'default');
});
