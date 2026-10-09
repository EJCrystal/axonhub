import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

// There is no DOM in this suite, so the row toggle is pinned by reading the
// component: clicking anywhere on a run's row has to expand it, otherwise the
// operator is forced to hit the small chevron on the far left.
const source = readFileSync(join(import.meta.dirname, 'intelligence-history-list.tsx'), 'utf8');

const rowStart = source.indexOf('<TableRow\n                  key={run.id}');
const row = rowStart === -1 ? '' : source.slice(rowStart, source.indexOf('</TableRow>', rowStart));

test('the run row itself toggles the detail', () => {
  assert.ok(row, 'the run row must exist');
  assert.match(row, /onClick=\{/, 'the row needs a click handler');
  assert.match(row, /setExpanded\(/, 'the row click must flip the expanded state');
  assert.match(row, /aria-expanded=\{isOpen\}/, 'the row must expose its expanded state');
  assert.match(row, /className='cursor-pointer'/, 'the row must look clickable');
});

// The chevron lives inside the row, so without stopping propagation one click
// would toggle twice and the row would appear stuck.
test('the chevron stops its click from also toggling the row', () => {
  assert.match(row, /event\.stopPropagation\(\)/, 'the inner button must stop propagation');
});

// The run and key badges paint with the verdict colour, so the three outcomes
// are distinguishable without reading the label. The in-flight badge is its own
// state and deliberately does not use a verdict colour.
test('every outcome badge uses the colour helper', () => {
  const badges = source.match(/<Badge[^>]*>/g) ?? [];
  assert.ok(badges.length >= 3, 'the running, run and key badges all render');

  const outcome = badges.filter((badge) => !badge.includes('sky'));
  assert.ok(outcome.length >= 2, 'the run and key outcome badges are present');
  for (const badge of outcome) {
    assert.match(badge, /verdictBadgeClass\(/, `badge must paint by verdict: ${badge}`);
  }

  assert.doesNotMatch(source, /verdictBadgeVariant/, 'the colourless variant must be gone');
});

// A run in flight has no outcome yet, so it gets its own badge and the duration
// column shows nothing rather than a misleading zero.
test('a running run shows a spinner badge instead of a verdict', () => {
  assert.match(source, /const inFlight = isRunInFlight\(run\)/);
  assert.match(source, /inFlight \? \(/, 'the row branches on it');
  assert.match(source, /intelligence\.history\.running/, 'the in-flight label is rendered');
  assert.match(source, /animate-spin/, 'and it is visibly in progress');
});

// Each history row now names the key it exercised, so a reader no longer has
// to expand a run to find out which credential produced the verdict.
test('the run row shows the api key column', () => {
  assert.match(source, /intelligence\.history\.columns\.apiKey/, 'the header names the column');
  assert.match(row, /runKeyLabel\(run\)/, 'the row renders the key label');
});

test('a run covering several keys lists every prefix', () => {
  const fn = source.slice(source.indexOf('function runKeyLabel'), source.indexOf('interface Props'));
  assert.match(fn, /run\.results\.map/, 'it reads the per-key results');
  assert.match(fn, /join\(', '\)/, 'several keys are all shown');
  assert.match(fn, /'—'/, 'a run with no key still renders a placeholder');
});

// The history view follows the test configuration: a channel removed there must
// stop appearing in the filter and in the merged channel tabs, otherwise the
// page keeps offering a channel the operator already deleted.
const page = readFileSync(join(import.meta.dirname, '..', 'index.tsx'), 'utf8');

test('the channel filter is built from the configured targets', () => {
  const opts = page.slice(page.indexOf('const configuredChannelIDs'), page.indexOf('const allRuns'));
  assert.match(opts, /config\?\.targets/, 'the ids come from the configuration');
  assert.match(opts, /channelIdKey\(target\.channelID\)/, 'a target id is normalised for comparison');

  assert.doesNotMatch(
    page,
    /channelOptions = useMemo\(\(\) => \(channels\?\.edges/,
    'the filter must not list every channel in the system'
  );
});

test('a removed channel is dropped from the merged groups', () => {
  const groups = page.slice(page.indexOf('const groups = useMemo'), page.indexOf('const selectedGroup'));
  assert.match(groups, /configuredChannelIDs\.includes\(String\(group\.channelID\)\)/, 'groups are filtered by the config');
});

// A run in flight has no outcome yet. The row is seeded with the key under test,
// so without a guard that seeded entry would render as a plain failure and the
// reader would see a verdict for a check that has not finished.
test('a running run does not show a verdict for its seeded key', () => {
  const detail = source.slice(source.indexOf('{run.results.map'));
  assert.match(detail, /inFlight \? \(/, 'the per-key badge branches on the in-flight state');

  // The seeded entry carries no quality and no html, so intelligenceVerdict
  // would answer "failed". That call has to sit after the in-flight branch.
  const badgeAt = detail.indexOf('verdictBadgeClass(intelligenceVerdict(key))');
  const branchAt = detail.indexOf('{inFlight ? (');
  assert.ok(branchAt !== -1 && badgeAt > branchAt, 'the verdict badge must be in the not-in-flight branch');

  assert.match(source, /!inFlight && \([\s\S]{0,40}?<IntelligenceHTMLPreview/, 'no preview until a result exists');
  assert.match(source, /!inFlight && canRecordManualVerdict\(key\)/, 'no manual verdict while in flight');
});
