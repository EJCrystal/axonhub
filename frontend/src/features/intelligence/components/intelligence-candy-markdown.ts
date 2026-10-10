// The candy answer is markdown with LaTeX: the model writes inline math as
// \( ... \) and display math as \[ ... \], the forms Claude uses. The markdown
// renderer in this app reads math with remark-math, which only understands the
// dollar delimiters and is configured with singleDollarTextMath off, so inline
// math has to use two dollars. Normalizing here rather than swapping the
// renderer's plugin set keeps its other defaults (gfm tables, bold, cjk line
// breaks) intact, which the answers also rely on.
//
// The conversion is deliberately text-in / text-out: the answer is prose the
// model wrote, and rewriting only the delimiters leaves the words untouched.
export function normalizeCandyAnswerMarkdown(answer: string): string {
  // Display math first: its delimiters must not be consumed by the inline
  // rule, which would leave a stray backslash-bracket in the text.
  const withDisplayMath = answer
    .replace(/\\\[([\s\S]*?)\\\]/g, (_match, body: string) => '\n\n$$\n' + body.trim() + '\n$$\n\n')
    .replace(/\\\(([\s\S]*?)\\\)/g, (_match, body: string) => '$$' + body.trim() + '$$');

  // Two formulas written back to back would read as one run, so space the
  // closing pair apart from the opening pair that follows it. Splitting on the
  // four-dollar run and rejoining handles every case at once, where a plain
  // replacement cannot tell a closing pair from an opening one.
  return withDisplayMath
    .split('$$$$')
    .join('$$ $$');
}
