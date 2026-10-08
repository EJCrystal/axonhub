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
const { intelligenceVerdict, runIntelligenceVerdict, canRecordManualVerdict, isRunInFlight } = await import(moduleUrl);
const { verdictBadgeVariant } = await import(moduleUrl);
const { verdictBadgeClass } = await import(moduleUrl);

const ALL_VERDICTS = ['normal', 'degraded', 'inconclusive', 'failed'];

test('a normal assessment maps to normal', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'normal' }), 'normal');
});

test('degraded and the historical suspicious label both map to degraded', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'degraded' }), 'degraded');
  assert.equal(intelligenceVerdict({ success: true, quality: 'suspicious' }), 'degraded');
});

// No automatic verdict with a page in hand is the case a person has to close
// out, so it reads as inconclusive rather than a failure.
test('an assessment nobody could settle, with a page, is inconclusive', () => {
  const html = '<html></html>';
  assert.equal(intelligenceVerdict({ success: true, quality: 'unknown', html }), 'inconclusive');
  assert.equal(intelligenceVerdict({ success: true, quality: '', html }), 'inconclusive');
  assert.equal(intelligenceVerdict({ success: false, quality: '', html }), 'inconclusive');
  assert.equal(intelligenceVerdict({ success: false, quality: 'normal', html }), 'inconclusive');
});

// The same outcomes without a page are plain failures, because there is nothing
// for a human to look at.
test('a key that never produced an answer is failed', () => {
  assert.equal(intelligenceVerdict({ success: false, quality: '' }), 'failed');
  assert.equal(intelligenceVerdict({ success: false, quality: 'normal' }), 'failed');
  assert.equal(intelligenceVerdict({ success: true, quality: 'unknown' }), 'failed');
  assert.equal(intelligenceVerdict({ success: true, quality: '', html: '' }), 'failed');
  assert.equal(intelligenceVerdict({ success: false, quality: '', html: '   ' }), 'failed');
  assert.equal(intelligenceVerdict({ success: false, quality: '', html: null }), 'failed');
});

// An oversized document has its html dropped by the backend, so a decisive
// automatic verdict must still be reported from the scoring alone.
test('a decisive verdict survives a dropped source', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'normal', html: '' }), 'normal');
  assert.equal(intelligenceVerdict({ success: true, quality: 'degraded', html: null }), 'degraded');
});

test('a manual verdict wins over the automatic result', () => {
  assert.equal(
    intelligenceVerdict({ success: false, quality: '', manualVerdict: 'degraded' }),
    'degraded',
    'a human can close out a run scoring could not decide'
  );
  assert.equal(
    intelligenceVerdict({ success: true, quality: 'degraded', manualVerdict: 'normal' }),
    'normal',
    'a human can overrule the classifier'
  );
});

test('an unrecognized manual verdict falls back to the automatic result', () => {
  assert.equal(intelligenceVerdict({ success: true, quality: 'normal', manualVerdict: 'whatever' }), 'normal');
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

test('a run with no successful key is failed', () => {
  assert.equal(runIntelligenceVerdict({ successKeys: 0, results: [{ success: false, quality: '' }] }), 'failed');
  assert.equal(runIntelligenceVerdict({ successKeys: 0, results: [] }), 'failed');
});

test('a run of unscored keys with pages is inconclusive', () => {
  assert.equal(
    runIntelligenceVerdict({
      successKeys: 1,
      results: [{ success: true, quality: 'unknown', html: '<html></html>' }],
    }),
    'inconclusive'
  );
});

// A run that generated nothing at all is a failure, not a question for a human.
test('a run with no generated pages is failed', () => {
  assert.equal(runIntelligenceVerdict({ results: [{ success: false, quality: '' }] }), 'failed');
});

// Degraded outranks inconclusive: a run with one degraded key is degraded even
// if another key could not be scored.
test('degraded outranks an unscored key', () => {
  assert.equal(
    runIntelligenceVerdict({
      results: [
        { success: true, quality: 'degraded', html: '<html></html>' },
        { success: true, quality: 'unknown', html: '<html></html>' },
      ],
    }),
    'degraded'
  );
});

test('a manual verdict lifts the run out of failed', () => {
  assert.equal(
    runIntelligenceVerdict({
      successKeys: 0,
      results: [{ success: false, quality: '', manualVerdict: 'degraded' }],
    }),
    'degraded'
  );
});

test('degraded is the alarming badge, normal the positive one, failed neutral', () => {
  assert.equal(verdictBadgeVariant('degraded'), 'destructive');
  assert.equal(verdictBadgeVariant('normal'), 'default');
  assert.equal(verdictBadgeVariant('failed'), 'secondary');
});

// The history table paints each outcome with its own colour so the three states
// are told apart at a glance: green normal, amber degraded, red failed.
test('each verdict maps to its own colour family', () => {
  assert.match(verdictBadgeClass('normal'), /emerald/, 'normal is green');
  assert.match(verdictBadgeClass('degraded'), /amber/, 'degraded is amber');
  assert.match(verdictBadgeClass('inconclusive'), /slate/, 'inconclusive is muted');
  assert.match(verdictBadgeClass('failed'), /red/, 'failed is red');

  // A dark-mode pair has to ship with each one, otherwise the badge is
  // unreadable in the theme the operator actually uses.
  for (const verdict of ALL_VERDICTS) {
    assert.match(verdictBadgeClass(verdict), /dark:/, `${verdict} needs a dark variant`);
  }

  const classes = new Set(ALL_VERDICTS.map(verdictBadgeClass));
  assert.equal(classes.size, ALL_VERDICTS.length, 'the verdicts must not share a colour');
});

// A manual verdict is only offered when there is a page to judge. A run that
// died before generating anything has nothing to review, so the buttons would
// ask the operator for a decision they cannot make.
test('a key with no generated page cannot be judged by hand', () => {
  assert.equal(canRecordManualVerdict({ success: false, quality: '', html: '' }), false, 'empty source');
  assert.equal(canRecordManualVerdict({ success: false, quality: '', html: null }), false, 'null source');
  assert.equal(canRecordManualVerdict({ success: false, quality: '', html: '   ' }), false, 'whitespace source');
  assert.equal(canRecordManualVerdict({ success: false, quality: '' }), false, 'absent source');
});

// The case the buttons exist for: the source is there, scoring could not settle
// it, so a human reads the page and decides.
test('a generated page whose scoring failed can be judged by hand', () => {
  assert.equal(canRecordManualVerdict({ success: false, quality: '', html: '<html></html>' }), true);
  assert.equal(canRecordManualVerdict({ success: true, quality: 'unknown', html: '<html></html>' }), true);
});

// Once a verdict is recorded the buttons give way to the clear action, and a
// key that already has a verdict needs no decision.
test('a key that already carries a verdict is not offered again', () => {
  assert.equal(canRecordManualVerdict({ success: false, quality: '', html: '<html></html>', manualVerdict: 'degraded' }), false);
  assert.equal(canRecordManualVerdict({ success: false, quality: '', html: '<html></html>', manualVerdict: 'normal' }), false);
});

// Keys that scored cleanly are not up for review either.
test('a normal or degraded automatic result needs no human decision', () => {
  assert.equal(canRecordManualVerdict({ success: true, quality: 'normal', html: '<html></html>' }), false);
  assert.equal(canRecordManualVerdict({ success: true, quality: 'degraded', html: '<html></html>' }), false);
});

// A run in flight has no outcome yet, so it must be told apart from a finished
// one; folding it into failed would claim a result that has not happened.
test('a running run is in flight, a finished one is not', () => {
  assert.equal(isRunInFlight({ status: 'running' }), true);
  for (const status of ['succeeded', 'failed', 'partial', '']) {
    assert.equal(isRunInFlight({ status }), false, status);
  }
  assert.equal(isRunInFlight({}), false, 'a missing status is not in flight');
});
