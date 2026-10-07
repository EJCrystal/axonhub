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

// Both the run badge and each key badge paint with the verdict colour, so the
// three outcomes are distinguishable without reading the label.
test('every verdict badge uses the colour helper', () => {
  const badges = source.match(/<Badge[^>]*>/g) ?? [];
  assert.ok(badges.length >= 2, 'the run and key badges must both render');
  for (const badge of badges) {
    assert.match(badge, /verdictBadgeClass\(/, `badge must paint by verdict: ${badge}`);
  }
  assert.doesNotMatch(source, /verdictBadgeVariant/, 'the colourless variant must be gone');
});
