import i18next from 'i18next';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { getExecutionModelAuditVerdict, getRequestModelAuditTooltip, getUpstreamModelAudit } from './upstream-model-audit.ts';

const resources = Object.fromEntries(
  ['en', 'zh-CN'].map((locale) => [
    locale,
    {
      translation: JSON.parse(readFileSync(new URL('../../../locales/' + locale + '/requests.json', import.meta.url), 'utf8')),
    },
  ])
);
const i18n = i18next.createInstance();
await i18n.init({ lng: 'zh-CN', fallbackLng: 'en', keySeparator: false, resources });

test('request 73927 reports every matching upstream model regardless of execution status', () => {
  const retry = { status: 'failed', outboundModelID: 'glm-5.3:free', upstreamModelID: 'glm-5.3:free' };
  const success = { status: 'completed', outboundModelID: 'glm-5.3', upstreamModelID: 'glm-5.3' };
  for (const executions of [
    [retry, retry, retry, success],
    [success, retry, retry, retry],
  ]) {
    const audit = getUpstreamModelAudit(executions);
    assert.equal(audit.status, 'matched');
    assert.deepEqual(new Set(audit.matchedUpstreamIds), new Set(['glm-5.3', 'glm-5.3:free']));
    for (const locale of ['en', 'zh-CN']) {
      const tooltip = getRequestModelAuditTooltip(audit, i18n.getFixedT(locale));
      assert.ok(tooltip.includes('glm-5.3'));
      assert.ok(tooltip.includes('glm-5.3:free'));
      assert.ok(!tooltip.includes('{{'));
    }
    for (const execution of executions) {
      const detail = getUpstreamModelAudit([execution]);
      assert.equal(detail.status, 'matched');
      assert.deepEqual(detail.upstreamModelIds, [execution.upstreamModelID]);
    }
  }
});

test('unknown retries retain their count alongside the successful model in both list languages', () => {
  const audit = getUpstreamModelAudit([
    { status: 'failed', outboundModelID: 'glm-5.3:free' },
    { status: 'completed', outboundModelID: 'glm-5.3', upstreamModelID: 'glm-5.3' },
  ]);
  for (const locale of ['en', 'zh-CN']) {
    const tooltip = getRequestModelAuditTooltip(audit, i18n.getFixedT(locale));
    assert.ok(tooltip.includes('glm-5.3'));
    assert.ok(tooltip.includes('1'));
    assert.ok(!tooltip.includes('glm-5.3:free'));
    assert.ok(!tooltip.includes('{{'));
  }
});

test('list warnings still display anomalies from failed retries after a successful match', () => {
  for (const upstreamModelIds of [['different'], ['sent', 'different']]) {
    const audit = getUpstreamModelAudit([
      { status: 'failed', outboundModelID: 'sent', upstreamModelIds },
      { status: 'completed', outboundModelID: 'glm-5.3', upstreamModelID: 'glm-5.3' },
    ]);
    for (const locale of ['en', 'zh-CN']) {
      assert.ok(getRequestModelAuditTooltip(audit, i18n.getFixedT(locale)).includes('different'));
    }
  }
});

test('failed matching evidence is valid model audit evidence', () => {
  const retry = { status: 'failed', outboundModelID: 'glm-5.3:free', upstreamModelID: 'glm-5.3:free' };
  const audit = getUpstreamModelAudit([retry]);
  assert.equal(audit.status, 'matched');
  assert.deepEqual(audit.matchedUpstreamIds, ['glm-5.3:free']);
  assert.ok(getRequestModelAuditTooltip(audit, i18n.t).includes('glm-5.3:free'));
});

test('request lifecycle does not override model match tooltips', () => {
  const audit = getUpstreamModelAudit([{ status: 'completed', outboundModelID: 'sent', upstreamModelID: 'sent' }]);
  const tooltip = getRequestModelAuditTooltip(audit, i18n.t);
  assert.ok(tooltip.includes('sent'));
  assert.ok(!tooltip.includes('请求未成功'));
  assert.ok(!tooltip.includes('请求处理中'));
});

test('successful matching retries stay matched without discarding failed or canceled unknowns', () => {
  for (const status of ['failed', 'canceled']) {
    const executions = [
      ...Array.from({ length: 12 }, () => ({ status, outboundModelID: 'sent' })),
      { status: 'completed', outboundModelID: 'sent', upstreamModelID: 'sent' },
    ];
    for (const ordered of [executions, executions.toReversed()]) {
      const audit = getUpstreamModelAudit(ordered);
      assert.equal(audit.status, 'matched');
      assert.equal(audit.unknownCount, 12);
      assert.equal(audit.comparedCount, 1);
    }
  }
});

test('any matching evidence establishes a match despite unknown executions', () => {
  for (const status of ['completed', 'pending', 'processing', undefined]) {
    const audit = getUpstreamModelAudit([
      { status: 'completed', outboundModelID: 'sent', upstreamModelID: 'sent' },
      { status, outboundModelID: 'sent' },
    ]);
    assert.equal(audit.status, 'matched');
    assert.equal(audit.comparedCount, 1);
    assert.equal(audit.unknownCount, 1);
  }
});

test('failed matching evidence remains matched when another failed execution is unknown', () => {
  const audit = getUpstreamModelAudit([
    { status: 'failed', outboundModelID: 'sent', upstreamModelID: 'sent' },
    { status: 'failed', outboundModelID: 'sent' },
  ]);
  assert.equal(audit.status, 'matched');
  assert.equal(audit.unknownCount, 1);
  assert.deepEqual(audit.matchedUpstreamIds, ['sent']);
});

test('successful retries do not hide earlier mismatches or conflicts', () => {
  for (const [upstreamModelIds, expected] of [
    [['different'], 'mismatched'],
    [['sent', 'different'], 'conflicting'],
  ]) {
    const audit = getUpstreamModelAudit([
      { status: 'failed', outboundModelID: 'sent', upstreamModelIds },
      { status: 'failed' },
      { status: 'completed', outboundModelID: 'sent', upstreamModelID: 'sent' },
    ]);
    assert.equal(audit.status, expected);
    assert.deepEqual(audit.mismatchedModelIds, ['different']);
    assert.equal(audit.unknownCount, 1);
  }
});

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

test('marks partially unknown executions as matched when known evidence matches', () => {
  const audit = getUpstreamModelAudit([{ outboundModelID: 'model-a', upstreamModelID: 'model-a' }, { outboundModelID: 'model-b' }]);
  assert.equal(audit.status, 'matched');
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

test('a failed execution with matching upstream evidence is green', () => {
  for (const execution of [
    { status: 'failed', outboundModelID: 'gpt-6-astra', upstreamModelID: 'gpt-6-astra', upstreamModelIds: ['gpt-6-astra'] },
    { status: 'failed', outboundModelID: 'glm-5.3:free', upstreamModelID: 'glm-5.3:free', upstreamModelIds: ['glm-5.3:free'] },
  ]) {
    const audit = getUpstreamModelAudit([execution]);
    assert.equal(audit.status, 'matched');
    assert.deepEqual(audit.matchedUpstreamIds, [execution.upstreamModelID]);
    for (const locale of ['en', 'zh-CN']) {
      const verdict = getExecutionModelAuditVerdict(audit, i18n.getFixedT(locale));
      assert.equal(verdict.tone, 'success');
      assert.equal(verdict.message, i18n.getFixedT(locale)('requests.detail.upstreamModelMatched'));
      assert.ok(!verdict.message.includes('{{'));
    }
  }
});

test('execution lifecycle never changes a matching model verdict', () => {
  for (const status of ['pending', 'processing', 'completed', 'failed', 'canceled', undefined]) {
    const audit = getUpstreamModelAudit([{ status, outboundModelID: 'sent', upstreamModelID: 'sent' }]);
    const verdict = getExecutionModelAuditVerdict(audit, i18n.getFixedT('zh-CN'));
    assert.equal(verdict.tone, 'success');
    assert.equal(verdict.message, i18n.t('requests.detail.upstreamModelMatched'));
  }
});

test('missing model evidence stays unknown regardless of execution lifecycle', () => {
  for (const status of ['pending', 'processing', 'completed', 'failed', 'canceled', undefined]) {
    const audit = getUpstreamModelAudit([{ status, outboundModelID: 'glm-5.3:free' }]);
    const verdict = getExecutionModelAuditVerdict(audit, i18n.getFixedT('zh-CN'));
    assert.equal(verdict.tone, 'muted');
    assert.equal(verdict.message, i18n.t('requests.tooltips.upstreamModelUnknown'));
  }
});

test('known mismatches and conflicts stay red regardless of execution lifecycle', () => {
  for (const status of ['pending', 'processing', 'completed', 'failed', 'canceled', undefined]) {
    const mismatch = getExecutionModelAuditVerdict(
      getUpstreamModelAudit([{ status, outboundModelID: 'sent', upstreamModelID: 'different' }]),
      i18n.getFixedT('zh-CN')
    );
    assert.equal(mismatch.tone, 'danger');
    assert.ok(mismatch.message.includes('different'));

    const conflict = getExecutionModelAuditVerdict(
      getUpstreamModelAudit([{ status, outboundModelID: 'sent', upstreamModelIds: ['sent', 'other'] }]),
      i18n.getFixedT('zh-CN')
    );
    assert.equal(conflict.tone, 'danger');
    assert.equal(conflict.message, i18n.t('requests.detail.upstreamModelConflict'));
  }
});

test('a stream mismatch remains visible even when a failed execution also matched once', () => {
  const mismatch = getExecutionModelAuditVerdict(
    getUpstreamModelAudit([
      { status: 'failed', outboundModelID: 'sent', upstreamModelID: 'sent' },
      { status: 'failed', outboundModelID: 'sent', upstreamModelID: 'different' },
    ]),
    i18n.getFixedT('zh-CN')
  );
  assert.equal(mismatch.tone, 'danger');
  assert.ok(mismatch.message.includes('different'));
});

test('each execution yields exactly one verdict and one locale key per tone', () => {
  const cases = [
    { outboundModelID: 'a', upstreamModelID: 'a' },
    { outboundModelID: 'a' },
    { outboundModelID: 'a', upstreamModelID: 'b' },
  ];
  for (const execution of cases) {
    for (const locale of ['en', 'zh-CN']) {
      const verdict = getExecutionModelAuditVerdict(getUpstreamModelAudit([execution]), i18n.getFixedT(locale));
      assert.equal(typeof verdict.message, 'string');
      assert.ok(verdict.message.length > 0);
      assert.ok(!verdict.message.includes('{{'));
      assert.ok(['success', 'danger', 'muted'].includes(verdict.tone));
    }
  }
});
