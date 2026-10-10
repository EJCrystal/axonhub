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

// The renderer this answer goes through paints a formula three times over —
// MathML, the TeX source, and the HTML copy — because the app carries neither
// KaTeX's stylesheet nor the class names its sanitize step would need to keep.
// Unwrapping the delimiters is what keeps the arithmetic readable as text, so
// the formula has to come out bare, with no delimiter and no backslash left.
test('inline math keeps the formula as plain text', () => {
  const out = normalizeCandyAnswerMarkdown('只有 ' + OPEN_PAREN + '6+4=10' + CLOSE_PAREN + ' 个。');
  assert.equal(out, '只有 6+4=10 个。');
});

test('display math keeps the formula on its own paragraph', () => {
  const out = normalizeCandyAnswerMarkdown('合计：' + OPEN_BRACKET + '\n7+9+12=28\n' + CLOSE_BRACKET + '结束。');
  assert.ok(out.includes('\n\n7+9+12=28\n\n'), 'the formula stands alone');
  assert.ok(!out.includes(OPEN_BRACKET), 'no display opener left');
  assert.ok(!out.includes(CLOSE_BRACKET), 'no display closer left');
});

// The defect this replaced was a formula that reached the renderer as math and
// came out as "7+9+12=287+9+12=28". Nothing in the output may look like math
// any more: neither the model's own delimiters nor the dollar pair Streamdown
// would treat as a formula.
test('no math delimiter survives either conversion', () => {
  const out = normalizeCandyAnswerMarkdown(
    '共 ' + OPEN_PAREN + '9+6+8+4=27' + CLOSE_PAREN + ' 个：' + OPEN_BRACKET + '7+9+12=28' + CLOSE_BRACKET + '\n'
  );
  assert.ok(!out.includes(OPEN_PAREN), 'no inline opener left');
  assert.ok(!out.includes(CLOSE_PAREN), 'no inline closer left');
  assert.ok(!out.includes(OPEN_BRACKET), 'no display opener left');
  assert.ok(!out.includes(CLOSE_BRACKET), 'no display closer left');
  assert.ok(!out.includes('$'), 'no dollar delimiter is introduced');
  assert.ok(out.includes('9+6+8+4=27'), 'the inline formula is still readable');
  assert.ok(out.includes('7+9+12=28'), 'the display formula is still readable');
});

// A double dollar pair would reach the renderer as math just like the
// backslash forms do, so it comes off the same way. A single pair is not math
// under this renderer's configuration and is left as the text the model wrote.
test('a double dollar pair is unwrapped, a single one is left alone', () => {
  const out = normalizeCandyAnswerMarkdown('共 $$7+9+12=28$$ 个，另有 $6+4=10$ 个。');
  assert.ok(!out.includes('$$'), 'no double dollar delimiter left');
  assert.ok(out.includes('7+9+12=28'), 'the display formula stays');
  assert.ok(out.includes('$6+4=10$'), 'a single dollar pair is not math here and stays as written');
});

// The answers also carry markdown the renderer handles on its own; the
// conversion must leave it alone.
test('markdown around the math is untouched', () => {
  const input = '**21 个一定够**\n\n- 只有 ' + OPEN_PAREN + '6+4=10' + CLOSE_PAREN + ' 个；\n\n| 配对 | 总数 |\n|---|---:|\n| 甲 | 21 |\n';
  const out = normalizeCandyAnswerMarkdown(input);
  assert.ok(out.includes('**21 个一定够**'), 'bold survives');
  assert.ok(out.includes('| 甲 | 21 |'), 'the table survives');
  assert.ok(out.includes('6+4=10'), 'the math is unwrapped');
});

// An answer without math is returned as written, so plain prose is not
// rewritten just because it went through the helper.
test('an answer without math is unchanged', () => {
  const plain = '最少需要取出 21 个。';
  assert.equal(normalizeCandyAnswerMarkdown(plain), plain);
});
