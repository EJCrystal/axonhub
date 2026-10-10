// The candy answer is prose the model wrote, with its arithmetic wrapped in
// LaTeX the way Claude writes it: the backslash-paren pair around inline math
// and the backslash-bracket pair around display math.
//
// That math must not reach the renderer as math. The markdown renderer in this
// app is Streamdown, whose KaTeX step emits every formula three times over —
// MathML, a TeX annotation, and an HTML copy — and then leans on KaTeX's
// stylesheet and its own class names to leave just one copy visible. The app
// ships no such stylesheet, and Streamdown's sanitize step strips the class
// names, so all three copies land on the page: one formula then reads as
// "7+9+12=287+9+12=28".
//
// The answers only ever put plain arithmetic between those delimiters, so the
// fix is to unwrap them: the formula survives as text, and the markdown around
// it — bold, lists, tables — still renders through Streamdown. Dollar
// delimiters are unwrapped too, so an answer that reaches for those cannot
// re-open the same hole.
//
// The conversion is deliberately text-in / text-out: the answer is prose the
// model wrote, and rewriting only the delimiters leaves the words untouched.
export function normalizeCandyAnswerMarkdown(answer: string): string {
  // Display math stands on its own: the model wrote it as a statement, and
  // leaving it inline would run it into the sentence before it.
  const withoutDisplayMath = answer
    .replace(/\$\$([\s\S]*?)\$\$/g, displayMathParagraph)
    .replace(/\\\[([\s\S]*?)\\\]/g, displayMathParagraph);

  return withoutDisplayMath.replace(/\\\(([\s\S]*?)\\\)/g, (_match, body: string) => body.trim());
}

// displayMathParagraph keeps a display formula on a paragraph of its own, with
// the blank lines markdown needs to start one.
function displayMathParagraph(_match: string, body: string): string {
  return '\n\n' + body.trim() + '\n\n';
}
