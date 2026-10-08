import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

// Load the real modules by transpiling them, so the assertions cannot drift
// from the implementation the page imports. The filter imports the verdict
// helper, so both get transpiled and linked through a small stub loader.
const dataDir = import.meta.dirname;
const componentDir = join(dataDir, '..', 'components');

function load(file) {
  const source = readFileSync(file, 'utf8');
  return ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
  }).outputText;
}

const verdictUrl = `data:text/javascript;base64,${Buffer.from(load(join(componentDir, 'intelligence-verdict.ts'))).toString('base64')}`;
const filterSource = load(join(dataDir, 'history-filter.ts')).replace("'../components/intelligence-verdict'", `'${verdictUrl}'`);
const filterUrl = `data:text/javascript;base64,${Buffer.from(filterSource).toString('base64')}`;
const { filterRuns, historyFilterOptions, isHistoryFilterActive, DEFAULT_HISTORY_FILTER } = await import(filterUrl);

const key = (keyPrefix, { success = true, quality = 'normal', manualVerdict = '', html = '' } = {}) => ({
  keyPrefix,
  success,
  quality,
  label: '',
  reason: '',
  taskID: '',
  generationMs: 0,
  durationMs: 0,
  html,
  error: null,
  manualVerdict,
});

// A page that exists but could not be scored: the case a human closes out.
const UNSCORED_HTML = '<html></html>';

const run = (id, { channelName = 'chan', modelID = 'm1', results }) => ({
  id,
  createdAt: '2026-10-06T00:00:00Z',
  channelID: 1,
  channelName,
  modelID,
  trigger: 'manual',
  status: 'succeeded',
  totalKeys: results.length,
  successKeys: results.filter((r) => r.success).length,
  failedKeys: results.filter((r) => !r.success).length,
  durationMs: 1000,
  results,
});

const DEGRADED_RUN = run('r-degraded', { results: [key('k1', { quality: 'degraded' })] });
const NORMAL_RUN = run('r-normal', { results: [key('k1', { quality: 'normal' })] });
const FAILED_RUN = run('r-failed', { results: [key('k1', { success: false, quality: '', error: 'boom' })] });
const INCONCLUSIVE_RUN = run('r-unscored', {
  results: [key('k1', { success: false, quality: '', html: UNSCORED_HTML, error: 'scoring unavailable' })],
});
const OTHER_MODEL = run('r-other', { modelID: 'm2', results: [key('k1', { quality: 'normal' })] });
const ALL = [DEGRADED_RUN, NORMAL_RUN, FAILED_RUN, INCONCLUSIVE_RUN, OTHER_MODEL];

test('the default filter keeps everything', () => {
  const out = filterRuns(ALL, DEFAULT_HISTORY_FILTER);
  assert.deepEqual(
    out.map((r) => r.id),
    ['r-degraded', 'r-normal', 'r-failed', 'r-unscored', 'r-other']
  );
  assert.equal(isHistoryFilterActive(DEFAULT_HISTORY_FILTER), false);
});

test('filtering by degraded returns only the degraded runs', () => {
  const out = filterRuns(ALL, { verdict: 'degraded' });
  assert.deepEqual(
    out.map((r) => r.id),
    ['r-degraded']
  );
  assert.equal(isHistoryFilterActive({ verdict: 'degraded' }), true);
});

// The two no-verdict buckets are separate: a run with no page is a failure,
// while one with a page waits on a human.
test('filtering by failed returns the runs that generated nothing', () => {
  const out = filterRuns(ALL, { verdict: 'failed' });
  assert.deepEqual(
    out.map((r) => r.id),
    ['r-failed']
  );
});

test('filtering by inconclusive returns the runs awaiting a human', () => {
  const out = filterRuns(ALL, { verdict: 'inconclusive' });
  assert.deepEqual(
    out.map((r) => r.id),
    ['r-unscored']
  );
});

test('filtering by model keeps only that model', () => {
  const out = filterRuns(ALL, { verdict: 'all', modelID: 'm2' });
  assert.deepEqual(
    out.map((r) => r.id),
    ['r-other']
  );
});

// Narrowing is what makes the key filter answer "what did this key do": the
// run's other keys are dropped and the counters describe what is left.
test('filtering by key narrows the run to that key and recomputes the counters', () => {
  const mixed = run('r-mixed', {
    results: [key('k1', { quality: 'degraded' }), key('k2', { quality: 'normal' })],
  });

  const out = filterRuns([mixed], { verdict: 'all', keyPrefix: 'k2' });
  assert.equal(out.length, 1);
  assert.deepEqual(
    out[0].results.map((r) => r.keyPrefix),
    ['k2']
  );
  assert.equal(out[0].totalKeys, 1);
  assert.equal(out[0].successKeys, 1);
  assert.equal(out[0].failedKeys, 0);
});

test('a run that never evaluated the key is dropped', () => {
  const out = filterRuns([DEGRADED_RUN], { verdict: 'all', keyPrefix: 'k-missing' });
  assert.deepEqual(out, []);
});

// The verdict filter follows the key, otherwise filtering by key on a degraded
// key would hide the run because the run as a whole still has a passing key.
test('verdict and key filters combine on the narrowed run', () => {
  const mixed = run('r-mixed', {
    results: [key('k1', { quality: 'degraded' }), key('k2', { quality: 'normal' })],
  });

  assert.deepEqual(
    filterRuns([mixed], { verdict: 'degraded', keyPrefix: 'k1' }).map((r) => r.id),
    ['r-mixed']
  );
  assert.deepEqual(filterRuns([mixed], { verdict: 'degraded', keyPrefix: 'k2' }), []);
});

test('a manual verdict is what the filter sees', () => {
  const manual = run('r-manual', { results: [key('k1', { success: false, quality: '', manualVerdict: 'degraded' })] });
  assert.deepEqual(
    filterRuns([manual], { verdict: 'degraded' }).map((r) => r.id),
    ['r-manual']
  );
});

test('the pickers only offer what the loaded runs contain', () => {
  const options = historyFilterOptions(ALL);
  assert.deepEqual(options.models, ['m1', 'm2']);
  assert.deepEqual(options.keys, ['k1']);
});
