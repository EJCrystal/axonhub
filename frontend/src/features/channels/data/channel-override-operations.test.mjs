import assert from 'node:assert/strict';
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { join as joinPath } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

const srcRoot = joinPath(import.meta.dirname, '..', '..', '..');
const schemaSource = readFileSync(joinPath(srcRoot, 'features/channels/data/schema.ts'), 'utf8');

const stubbed = schemaSource
  .replace(
    /^import \{ pageInfoSchema \} from '@\/gql\/pagination';$/m,
    "import { z as zodForStub } from 'zod';\nconst pageInfoSchema = zodForStub.object({ hasNextPage: zodForStub.boolean(), hasPreviousPage: zodForStub.boolean(), startCursor: zodForStub.string().optional(), endCursor: zodForStub.string().optional() });"
  )
  .replace(/^import \{ z \} from 'zod';$/m, "import { z } from 'zod';\n");

const outFile = joinPath(import.meta.dirname, `.schema-under-test-ops-${process.pid}.mjs`);
writeFileSync(
  outFile,
  ts.transpileModule(stubbed, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText
);

const { overrideOperationSchema, channelSettingsSchema } = await import(pathToFileURL(outFile).href);
test.after(() => rmSync(outFile, { force: true }));

test('overrideOperationSchema accepts GraphQL null fields and normalizes them to undefined', () => {
  const graphqlOperation = {
    op: 'set',
    path: 'reasoning.effort',
    from: null,
    to: null,
    value: 'max',
    condition: null,
    match: null,
    index: null,
    splat: null,
  };

  const parsed = overrideOperationSchema.parse(graphqlOperation);
  assert.equal(parsed.op, 'set');
  assert.equal(parsed.path, 'reasoning.effort');
  assert.equal(parsed.value, 'max');
  assert.equal(parsed.from, undefined);
  assert.equal(parsed.to, undefined);
  assert.equal(parsed.condition, undefined);
  assert.equal(parsed.match, undefined);
  assert.equal(parsed.index, undefined);
  assert.equal(parsed.splat, undefined);
});

test('channelSettingsSchema accepts bodyOverrideOperations and headerOverrideOperations with GraphQL nulls', () => {
  const graphqlSettings = {
    extraModelPrefix: '',
    bodyOverrideOperations: [
      {
        op: 'set',
        path: 'reasoning.effort',
        from: null,
        to: null,
        value: 'max',
        condition: '{{eq .Model "gpt-5.6-luna"}}',
        match: null,
        index: null,
        splat: null,
      },
    ],
    headerOverrideOperations: [
      {
        op: 'set',
        path: 'User-Agent',
        from: null,
        to: null,
        value: 'codex-tui/0.162.1',
        condition: null,
        match: null,
        index: null,
        splat: null,
      },
    ],
  };

  const parsed = channelSettingsSchema.parse(graphqlSettings);
  assert.equal(parsed.bodyOverrideOperations?.length, 1);
  assert.equal(parsed.bodyOverrideOperations?.[0].path, 'reasoning.effort');
  assert.equal(parsed.bodyOverrideOperations?.[0].from, undefined);
  assert.equal(parsed.bodyOverrideOperations?.[0].to, undefined);
  assert.equal(parsed.headerOverrideOperations?.length, 1);
  assert.equal(parsed.headerOverrideOperations?.[0].path, 'User-Agent');
  assert.equal(parsed.headerOverrideOperations?.[0].condition, undefined);
});
