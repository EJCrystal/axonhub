import assert from 'node:assert/strict';
import test from 'node:test';
import { getUpstreamModelAudit } from './upstream-model-audit.ts';

test('matches each execution against its own outbound model', () => {
  const audit = getUpstreamModelAudit([
    { modelID: 'model-a', upstreamModelID: 'model-a' },
    { modelID: 'model-b', upstreamModelID: 'model-b' },
  ]);
  assert.equal(audit.status, 'matched');
  assert.equal(audit.comparedCount, 2);
  assert.equal(audit.unknownCount, 0);
});

test('does not match a retry against a previous execution model', () => {
  const audit = getUpstreamModelAudit([
    { modelID: 'model-a', upstreamModelID: null },
    { modelID: 'model-b', upstreamModelID: 'model-a' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.mismatchedModelIds, ['model-a']);
  assert.equal(audit.unknownCount, 1);
});

test('detects swapped models even when both appear in the outbound model set', () => {
  const audit = getUpstreamModelAudit([
    { modelID: 'model-a', upstreamModelID: 'model-b' },
    { modelID: 'model-b', upstreamModelID: 'model-a' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.mismatchedModelIds, ['model-b', 'model-a']);
});

test('does not mark partially unknown executions as matched', () => {
  const audit = getUpstreamModelAudit([
    { modelID: 'model-a', upstreamModelID: 'model-a' },
    { modelID: 'model-b' },
  ]);
  assert.equal(audit.status, 'unknown');
  assert.equal(audit.comparedCount, 1);
  assert.equal(audit.unknownCount, 1);
});

test('keeps empty, historical, and missing-model executions unknown', () => {
  assert.equal(getUpstreamModelAudit([]).status, 'unknown');
  for (const execution of [
    { modelID: 'model-a' },
    { modelID: 'model-a', upstreamModelID: null },
    { modelID: 'model-a', upstreamModelID: '' },
    { modelID: 'model-a', upstreamModelID: '   ' },
    { modelID: '', upstreamModelID: 'model-a' },
  ]) {
    const audit = getUpstreamModelAudit([execution]);
    assert.equal(audit.status, 'unknown');
    assert.equal(audit.unknownCount, 1);
    assert.equal(audit.comparedCount, 0);
  }
});

test('preserves exact reported names instead of normalizing versions or case', () => {
  const audit = getUpstreamModelAudit([
    { modelID: 'model-a', upstreamModelID: 'Model-A' },
    { modelID: 'model-a', upstreamModelID: 'model-a-2026-09-19' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.upstreamModelIds, ['Model-A', 'model-a-2026-09-19']);
});

test('deduplicates reported names only after retaining every execution mismatch', () => {
  const audit = getUpstreamModelAudit([
    { modelID: 'model-a', upstreamModelID: 'model-a' },
    { modelID: 'model-b', upstreamModelID: 'model-a' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.upstreamModelIds, ['model-a']);
  assert.deepEqual(audit.mismatchedModelIds, ['model-a']);
});
