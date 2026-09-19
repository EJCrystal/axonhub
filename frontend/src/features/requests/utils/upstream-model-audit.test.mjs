import assert from 'node:assert/strict';
import test from 'node:test';
import { getUpstreamModelAudit } from './upstream-model-audit.ts';

test('matches each execution against its own outbound model', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: 'model-a' },
    { outboundModelID: 'model-b', upstreamModelID: 'model-b' },
  ]);
  assert.equal(audit.status, 'matched');
  assert.equal(audit.comparedCount, 2);
  assert.equal(audit.unknownCount, 0);
});

test('does not match a retry against a previous execution model', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: null },
    { outboundModelID: 'model-b', upstreamModelID: 'model-a' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.mismatchedModelIds, ['model-a']);
  assert.equal(audit.unknownCount, 1);
});

test('detects swapped models even when both appear in the outbound model set', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: 'model-b' },
    { outboundModelID: 'model-b', upstreamModelID: 'model-a' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.mismatchedModelIds, ['model-b', 'model-a']);
});

test('does not mark partially unknown executions as matched', () => {
  const audit = getUpstreamModelAudit([{ outboundModelID: 'model-a', upstreamModelID: 'model-a' }, { outboundModelID: 'model-b' }]);
  assert.equal(audit.status, 'unknown');
  assert.equal(audit.comparedCount, 1);
  assert.equal(audit.unknownCount, 1);
});

test('keeps empty, historical, and missing-model executions unknown', () => {
  assert.equal(getUpstreamModelAudit([]).status, 'unknown');
  for (const execution of [
    { outboundModelID: 'model-a' },
    { outboundModelID: 'model-a', upstreamModelID: null },
    { outboundModelID: 'model-a', upstreamModelID: '' },
    { outboundModelID: 'model-a', upstreamModelID: '   ' },
    { outboundModelID: '', upstreamModelID: 'model-a' },
  ]) {
    const audit = getUpstreamModelAudit([execution]);
    assert.equal(audit.status, 'unknown');
    assert.equal(audit.unknownCount, 1);
    assert.equal(audit.comparedCount, 0);
  }
});

test('preserves exact reported names instead of normalizing versions or case', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: 'Model-A' },
    { outboundModelID: 'model-a', upstreamModelID: 'model-a-2026-09-19' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.upstreamModelIds, ['Model-A', 'model-a-2026-09-19']);
});

test('deduplicates reported names only after retaining every execution mismatch', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: 'model-a' },
    { outboundModelID: 'model-b', upstreamModelID: 'model-a' },
  ]);
  assert.equal(audit.status, 'mismatched');
  assert.deepEqual(audit.upstreamModelIds, ['model-a']);
  assert.deepEqual(audit.mismatchedModelIds, ['model-a']);
});

test('uses the final sent model after channel overrides, preserving the routing name', () => {
  const execution = { modelID: 'routed-a', outboundModelID: 'sent-b' };
  assert.equal(getUpstreamModelAudit([{ ...execution, upstreamModelID: 'sent-b' }]).status, 'matched');
  assert.equal(getUpstreamModelAudit([{ ...execution, upstreamModelID: 'routed-a' }]).status, 'mismatched');
  const historical = getUpstreamModelAudit([{ modelID: 'routed-a', upstreamModelID: 'routed-a' }]);
  assert.equal(historical.status, 'unknown');
  assert.deepEqual(historical.upstreamModelIds, ['routed-a']);
});

test('retains conflicting names from one stream even when its first model matches', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: 'model-a', upstreamModelIds: ['model-a', 'model-b'] },
  ]);
  assert.equal(audit.status, 'conflicting');
  assert.equal(audit.conflictCount, 1);
  assert.deepEqual(audit.conflictingModelIds, ['model-a', 'model-b']);
  assert.deepEqual(audit.mismatchedModelIds, ['model-b']);
});

test('reports a stream conflict even when the final sent model is unknown', () => {
  const audit = getUpstreamModelAudit([{ upstreamModelIds: ['model-a', 'model-b'] }]);
  assert.equal(audit.status, 'conflicting');
  assert.equal(audit.unknownCount, 1);
  assert.equal(audit.comparedCount, 0);
  assert.deepEqual(audit.conflictingModelIds, ['model-a', 'model-b']);
});

test('does not confuse retry model changes or duplicate observations with a stream conflict', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelID: 'model-a', upstreamModelIds: ['model-a', 'model-a'] },
    { outboundModelID: 'model-b', upstreamModelIds: ['model-b'] },
  ]);
  assert.equal(audit.status, 'matched');
  assert.equal(audit.conflictCount, 0);
  assert.deepEqual(audit.upstreamModelIds, ['model-a', 'model-b']);
});

test('a conflict does not hide other known mismatches or unknown executions', () => {
  const audit = getUpstreamModelAudit([
    { outboundModelID: 'model-a', upstreamModelIds: ['model-a', 'model-b'] },
    { outboundModelID: 'model-c', upstreamModelID: 'model-d' },
    { modelID: 'historical', upstreamModelID: 'historical' },
  ]);
  assert.equal(audit.status, 'conflicting');
  assert.equal(audit.unknownCount, 1);
  assert.equal(audit.comparedCount, 2);
  assert.deepEqual(audit.mismatchedModelIds, ['model-b', 'model-d']);
});
