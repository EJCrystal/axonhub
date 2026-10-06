import assert from 'node:assert/strict';
import { readFileSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

// Load the real schema by transpiling it, so the assertions cannot drift from
// what the history page parses. The transpiled copy is written inside the
// project so Node can still resolve the module's "zod" import, then removed.
const dataDir = import.meta.dirname;
const tempPath = join(dataDir, '__schema.test.tmp.mjs');
const transpiled = ts.transpileModule(readFileSync(join(dataDir, 'schema.ts'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
}).outputText;
writeFileSync(tempPath, transpiled);

const { intelligenceRunConnectionSchema, intelligenceRunSchema } = await import(`./__schema.test.tmp.mjs`);
unlinkSync(tempPath);

// The backend serializes absent optional strings as null, not undefined. This
// is the shape the server actually returned for a failed run with a pending
// manual verdict; a bare default('') on any of these fields used to fail the
// whole parse, which left the page claiming there were no records at all.
const FAILED_RUN = {
  id: 'gid://axonhub/IntelligenceRun/22',
  createdAt: '2026-10-06T14:46:47.833387Z',
  channelID: 6,
  channelName: 'OpenAI-皓悦',
  modelID: 'gpt-6.1-sol',
  trigger: 'scheduled',
  status: 'failed',
  totalKeys: 1,
  successKeys: 0,
  failedKeys: 1,
  durationMs: 378598,
  results: [
    {
      keyPrefix: 'sk-9****9c31',
      success: false,
      quality: '',
      label: '',
      reason: '',
      taskID: '',
      generationMs: 0,
      durationMs: 378597,
      html: null,
      error: 'Upstream request failed',
      manualVerdict: null,
    },
  ],
};

test('a run whose optional fields are null still parses', () => {
  const parsed = intelligenceRunSchema.parse(FAILED_RUN);
  assert.equal(parsed.results[0].html, '', 'null html normalizes to empty');
  assert.equal(parsed.results[0].manualVerdict, '', 'null manual verdict normalizes to empty');
  assert.equal(parsed.results[0].error, 'Upstream request failed');
});

test('a connection of runs with nulls parses', () => {
  const connection = {
    totalCount: 2,
    edges: [
      { cursor: 'c1', node: FAILED_RUN },
      {
        cursor: 'c2',
        node: {
          ...FAILED_RUN,
          id: 'gid://axonhub/IntelligenceRun/21',
          status: 'succeeded',
          successKeys: 1,
          failedKeys: 0,
          results: [
            { ...FAILED_RUN.results[0], success: true, quality: 'degraded', error: null, manualVerdict: 'degraded' },
          ],
        },
      },
    ],
    pageInfo: { hasNextPage: false, hasPreviousPage: false, startCursor: 'c1', endCursor: 'c2' },
  };

  const parsed = intelligenceRunConnectionSchema.parse(connection);
  assert.equal(parsed.edges.length, 2);
  assert.equal(parsed.edges[1].node.results[0].manualVerdict, 'degraded');
});

// A missing field must not fail either; older rows predate manualVerdict.
test('a result without the newer fields still parses', () => {
  const legacy = {
    ...FAILED_RUN,
    results: [
      {
        keyPrefix: 'sk-9****9c31',
        success: true,
        quality: 'normal',
        label: '',
        reason: '',
        taskID: '',
        generationMs: 0,
        durationMs: 0,
        html: '<html></html>',
      },
    ],
  };

  const parsed = intelligenceRunSchema.parse(legacy);
  assert.equal(parsed.results[0].manualVerdict, '');
  assert.equal(parsed.results[0].error, undefined);
});
