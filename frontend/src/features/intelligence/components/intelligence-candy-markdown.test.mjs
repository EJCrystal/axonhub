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
// KaTeX's stylesheet nor the class names its sanitize step needs to keep. The
// math is therefore reduced to text, and nothing of LaTeX may be left behind.
test('inline math is reduced to the text it stands for', () => {
  const out = normalizeCandyAnswerMarkdown('只有 ' + OPEN_PAREN + '6+4=10' + CLOSE_PAREN + ' 个。');
  assert.equal(out, '只有 6+4=10 个。');
});

// The whole reason the answer goes through this helper: the symbols the model
// writes as commands have to read as the characters a person expects.
test('symbol commands become their characters', () => {
  const out = normalizeCandyAnswerMarkdown('当 ' + OPEN_PAREN + 'x ' + BACKSLASH + 'le 21' + CLOSE_PAREN + ' 时成立。');
  assert.match(out, /x ≤ 21/, 'a relation reads as its symbol');
});

// A fraction is arithmetic a reader can follow, so it stays readable rather
// than turning into braces around two numbers.
test('a fraction becomes a slash', () => {
  const out = normalizeCandyAnswerMarkdown('平均 ' + OPEN_PAREN + BACKSLASH + 'frac{42}{2} = 21' + CLOSE_PAREN + ' 个。');
  assert.match(out, /42\/2 = 21/, 'the fraction is a division');
});

// The wrappers are the commands that actually show up in the answers, and they
// nest, so the body has to survive every layer.
test('nested wrappers keep the value they wrap', () => {
  const out = normalizeCandyAnswerMarkdown('答案：' + OPEN_BRACKET + BACKSLASH + 'boxed{29' + BACKSLASH + 'text{ 颗}}' + CLOSE_BRACKET);
  assert.match(out, /29 颗/, 'the wrapped value is what the reader sees');
  assert.ok(!out.includes('boxed') && !out.includes('text'), 'the wrappers are gone');
});

// A display formula stands on its own and is not followed by a run of blank
// lines, which would read as a hole in the answer.
test('display math keeps the formula on one padded paragraph', () => {
  const out = normalizeCandyAnswerMarkdown('合计：' + OPEN_BRACKET + '\n7+9+12=28\n' + CLOSE_BRACKET + '结束。');
  assert.ok(out.includes('\n\n7+9+12=28\n\n'), 'the formula stands alone');
  assert.ok(!out.includes('\n\n\n'), 'and is not padded twice');
});

// The defect this replaced was a formula that reached the renderer as math and
// came out as "7+9+12=287+9+12=28". Nothing may look like math any more, and no
// delimiter or command may survive to be shown as typed.
test('no math syntax survives, whatever the model wrote', () => {
  const out = normalizeCandyAnswerMarkdown(
    '共 ' + OPEN_PAREN + '9+6+8+4=27' + CLOSE_PAREN + ' 个：' + OPEN_BRACKET + BACKSLASH + 'boxed{7+9+12=28}' + CLOSE_BRACKET + '\n'
  );

  const leftovers = [OPEN_PAREN, CLOSE_PAREN, OPEN_BRACKET, CLOSE_BRACKET, '$$', '{', '}', BACKSLASH];
  for (const leftover of leftovers) {
    assert.ok(!out.includes(leftover), 'no leftover: ' + leftover);
  }

  assert.ok(out.includes('9+6+8+4=27'), 'the inline formula is still readable');
  assert.ok(out.includes('7+9+12=28'), 'the display formula is still readable');
});

// An unknown command is a command the model reached for that this helper has no
// character for. Its name is kept, because dropping it would delete content;
// what it must not do is keep the backslash.
test('an unknown command keeps its name but loses the backslash', () => {
  const out = normalizeCandyAnswerMarkdown('取 ' + OPEN_PAREN + BACKSLASH + 'mathbin{+} 21' + CLOSE_PAREN + ' 个。');
  assert.ok(!out.includes(BACKSLASH), 'no backslash left');
  assert.ok(out.includes('21'), 'the value is untouched');
});

// The answers also carry markdown the renderer handles on its own; the
// conversion must leave it alone.
test('markdown around the math is untouched', () => {
  const input = '**21 个一定够**\n\n- 只有 ' + OPEN_PAREN + '6+4=10' + CLOSE_PAREN + ' 个；\n\n| 配对 | 总数 |\n|---|---:|\n| 甲 | 21 |\n';
  const out = normalizeCandyAnswerMarkdown(input);
  assert.ok(out.includes('**21 个一定够**'), 'bold survives');
  assert.ok(out.includes('| 甲 | 21 |'), 'the table survives');
  assert.ok(out.includes('6+4=10'), 'the math is reduced');
});

// A backslash in the reader's own prose is not math and must be left alone, or
// a path or an escape in the model's words would be rewritten.
test('prose outside the math is not rewritten', () => {
  const plain = '见 C:' + BACKSLASH + 'temp 里的文件，最少需要取出 21 个。';
  assert.equal(normalizeCandyAnswerMarkdown(plain), plain);
});

// An answer without math is returned as written.
test('an answer without math is unchanged', () => {
  const plain = '最少需要取出 21 个。';
  assert.equal(normalizeCandyAnswerMarkdown(plain), plain);
});
