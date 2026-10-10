import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import ts from 'typescript';

// Load the real module by transpiling it, so the assertions cannot drift from
// the helper the candy answer renders through.
const componentDir = import.meta.dirname;
const source = readFileSync(join(componentDir, 'intelligence-candy-markdown.ts'), 'utf8');
const transpiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2023 },
}).outputText;
const moduleUrl = 'data:text/javascript;base64,' + Buffer.from(transpiled).toString('base64');
const { normalizeCandyAnswerMarkdown } = await import(moduleUrl);

const BACKSLASH = String.fromCharCode(92);
const OPEN_PAREN = BACKSLASH + '(';
const CLOSE_PAREN = BACKSLASH + ')';
const OPEN_BRACKET = BACKSLASH + '[';
const CLOSE_BRACKET = BACKSLASH + ']';

// The renderer's math plugin understands only dollar delimiters, so the forms
// the model actually writes have to come out as dollars and nothing else.
test('inline math becomes a dollar pair', () => {
  const out = normalizeCandyAnswerMarkdown('只有 ' + OPEN_PAREN + '6+4=10' + CLOSE_PAREN + ' 个。');
  assert.equal(out, '只有 $$6+4=10$$ 个。');
});

test('display math becomes a dollar block around the formula', () => {
  const out = normalizeCandyAnswerMarkdown('合计：' + OPEN_BRACKET + '\n7+9+12=28\n' + CLOSE_BRACKET + '结束。');
  assert.match(out, /\$\$\n7\+9\+12=28\n\$\$/, 'the formula sits in a dollar block');
  assert.ok(!out.includes(OPEN_BRACKET), 'no display opener left');
  assert.ok(!out.includes(CLOSE_BRACKET), 'no display closer left');
});

// A display block handled by the inline rule first would leave the stray
// backslash-bracket behind, which is the whole defect being fixed.
test('no raw latex delimiter survives either conversion', () => {
  const out = normalizeCandyAnswerMarkdown(
    '共 ' + OPEN_PAREN + '9+6+8+4=27' + CLOSE_PAREN + ' 个：' + OPEN_BRACKET + '7+9+12=28' + CLOSE_BRACKET + '\n'
  );
  assert.ok(!out.includes(OPEN_PAREN), 'no inline opener left');
  assert.ok(!out.includes(CLOSE_PAREN), 'no inline closer left');
  assert.ok(!out.includes(OPEN_BRACKET), 'no display opener left');
  assert.ok(!out.includes(CLOSE_BRACKET), 'no display closer left');
  assert.ok(out.includes('$$9+6+8+4=27$$'), 'the inline formula is a dollar pair');
  assert.ok(out.includes('$$\n7+9+12=28\n$$'), 'the display formula is a dollar block');
});

// Two formulas written back to back would parse as one, so a space keeps each
// a formula of its own.
test('adjacent inline formulas stay separate', () => {
  const out = normalizeCandyAnswerMarkdown(OPEN_PAREN + 'a' + CLOSE_PAREN + OPEN_PAREN + 'b' + CLOSE_PAREN);
  assert.equal(out, '$$a$$ $$b$$');
});

// The answers also carry markdown the renderer handles on its own; the
// conversion must leave it alone.
test('markdown around the math is untouched', () => {
  const input = '**21 个一定够**\n\n- 只有 ' + OPEN_PAREN + '6+4=10' + CLOSE_PAREN + ' 个；\n\n| 配对 | 总数 |\n|---|---:|\n| 甲 | 21 |\n';
  const out = normalizeCandyAnswerMarkdown(input);
  assert.ok(out.includes('**21 个一定够**'), 'bold survives');
  assert.ok(out.includes('| 甲 | 21 |'), 'the table survives');
  assert.ok(out.includes('$$6+4=10$$'), 'the math is converted');
});

// An answer without math is returned as written, so plain prose is not
// rewritten just because it went through the helper.
test('an answer without math is unchanged', () => {
  const plain = '最少需要取出 21 个。';
  assert.equal(normalizeCandyAnswerMarkdown(plain), plain);
});
