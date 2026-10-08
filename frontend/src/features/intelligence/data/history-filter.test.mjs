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
const { filterRuns, historyFilterOptions, isHistoryFilterActive, DEFAULT_HISTORY_FILTER, groupRunsByChannel, pickActiveChannel } =
  await import(filterUrl);

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

const run = (id, { channelName = 'chan', modelID = 'm1', reasoningEffort = '', results }) => ({
  id,
  createdAt: '2026-10-06T00:00:00Z',
  channelID: 1,
  channelName,
  modelID,
  reasoningEffort,
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

// The level is part of what a run tested, so it filters like the model does.
// The empty string is a real bucket: runs recorded before the field existed,
// and runs that used the provider default.
test('filtering by thinking level keeps only that level', () => {
  const high = run('r-high', { reasoningEffort: 'high', results: [key('k1')] });
  const low = run('r-low', { reasoningEffort: 'low', results: [key('k1')] });
  const unset = run('r-unset', { reasoningEffort: '', results: [key('k1')] });

  const all = [high, low, unset];

  assert.deepEqual(
    filterRuns(all, { verdict: 'all', reasoningEffort: 'high' }).map((r) => r.id),
    ['r-high']
  );
  assert.deepEqual(
    filterRuns(all, { verdict: 'all', reasoningEffort: 'low' }).map((r) => r.id),
    ['r-low']
  );
  assert.deepEqual(
    filterRuns(all, { verdict: 'all', reasoningEffort: '' }).map((r) => r.id),
    ['r-unset'],
    'the unset level is its own bucket, not everything'
  );

  // undefined is the absence of a filter, which must keep everything.
  assert.equal(filterRuns(all, { verdict: 'all' }).length, 3);
});

test('the effort filter counts as an active filter', () => {
  assert.equal(isHistoryFilterActive({ verdict: 'all', reasoningEffort: 'high' }), true);
  assert.equal(isHistoryFilterActive({ verdict: 'all', reasoningEffort: '' }), true, 'unset is a choice');
  assert.equal(isHistoryFilterActive({ verdict: 'all' }), false);
});

test('the level picker only offers levels the loaded runs used', () => {
  const runs = [
    run('r-high', { reasoningEffort: 'high', results: [key('k1')] }),
    run('r-low', { reasoningEffort: 'low', results: [key('k1')] }),
    run('r-unset', { reasoningEffort: '', results: [key('k1')] }),
  ];

  // The unset runs contribute nothing: the UI offers that bucket on its own.
  assert.deepEqual(historyFilterOptions(runs).efforts, ['high', 'low']);
});

// The merged view shows one tab per channel. The order and membership of those
// tabs is what the grouping decides, newest-first runs preserved.
test('runs group by channel, newest first and in first-seen order', () => {
  const a1 = run('a1', { results: [key('k1')] });
  const a1b = { ...a1, channelID: 1 };
  const b1 = { ...a1, id: 'b1', channelID: 2 };
  const a2 = { ...a1, id: 'a2', channelID: 1 };

  const groups = groupRunsByChannel([a1b, b1, a2]);
  assert.deepEqual(
    groups.map((g) => g.channelID),
    [1, 2],
    'first-seen order'
  );
  assert.deepEqual(
    groups[0].runs.map((r) => r.id),
    ['a1', 'a2'],
    'newest first within a channel'
  );
  assert.deepEqual(
    groups[1].runs.map((r) => r.id),
    ['b1']
  );
});

test('no runs produce no groups', () => {
  assert.deepEqual(groupRunsByChannel([]), []);
});

// The default tab is the first channel, and a filter that removes the selected
// channel must not leave the view blank.
test('the active channel defaults to the first and falls back when it disappears', () => {
  const groups = [
    { channelID: 6, channelName: 'a', runs: [] },
    { channelID: 2, channelName: 'b', runs: [] },
  ];

  assert.equal(pickActiveChannel(groups, '')?.channelID, 6, 'nothing chosen yet -> the first');
  assert.equal(pickActiveChannel(groups, '2')?.channelID, 2, 'a live choice wins');
  assert.equal(pickActiveChannel(groups, '404')?.channelID, 6, 'a vanished choice falls back to the first');
  assert.equal(pickActiveChannel([], '6'), undefined, 'no groups -> nothing to show');
});
