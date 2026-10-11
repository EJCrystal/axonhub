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
// So the math is reduced to plain text instead. Unwrapping the delimiters alone
// is not enough: the bodies carry commands of their own — \boxed, \text, \le,
// \quad and whatever the model reaches for next — and those would surface as
// backslashes and braces. Each formula therefore goes through latexToText,
// which drops the wrappers, maps the symbols that have a character to it, and
// lets anything unrecognised lose its backslash rather than its content, so an
// unknown command still reads as words instead of vanishing.
//
// Only the formulas are rewritten. The prose around them is left alone, so the
// markdown the renderer does handle well — bold, lists, tables — still renders
// through Streamdown, and a backslash in the reader's own words is untouched.
//
// The conversion is deliberately text-in / text-out: the answer is prose the
// model wrote, and rewriting only the math leaves the words as written.

// Commands whose whole job is to wrap their argument. The argument is what the
// reader wants, so the wrapper comes off and the body stays.
const LATEX_WRAPPERS = new Set([
  'boxed',
  'fbox',
  'text',
  'textbf',
  'textit',
  'textrm',
  'textsf',
  'texttt',
  'mathrm',
  'mathbf',
  'mathit',
  'mathsf',
  'mathtt',
  'mathcal',
  'operatorname',
  'displaystyle',
  'textstyle',
  'scriptstyle',
  'mbox',
  'hbox',
  'mathord',
  'mathbin',
  'mathrel',
  'mathopen',
  'mathclose',
  'mathpunct',
]);

// Commands that only set the size or spacing of the delimiter after them. They
// carry no content, so they are removed outright.
const LATEX_IGNORED = new Set([
  'left',
  'right',
  'middle',
  'big',
  'Big',
  'bigg',
  'Bigg',
  'bigl',
  'bigr',
  'Bigl',
  'Bigr',
  'biggl',
  'biggr',
  'Biggl',
  'Biggr',
  'limits',
  'nolimits',
  'phantom',
  'hphantom',
  'vphantom',
  'quad',
  'qquad',
  'thinspace',
  'enspace',
]);

// Commands that stand for a single character. The answers are arithmetic, so
// this is the set that actually comes up; anything else falls through to the
// rule that keeps the name as words.
const LATEX_SYMBOLS: Record<string, string> = {
  le: '≤',
  leq: '≤',
  ge: '≥',
  geq: '≥',
  neq: '≠',
  ne: '≠',
  equiv: '≡',
  approx: '≈',
  times: '×',
  cdot: '·',
  div: '÷',
  pm: '±',
  mp: '∓',
  to: '→',
  rightarrow: '→',
  leftarrow: '←',
  infty: '∞',
  degree: '°',
  circ: '°',
  sum: 'Σ',
  prod: 'Π',
  // Escapes for the characters that are special in LaTeX.
  '%': '%',
  '&': '&',
  '#': '#',
  $: '$',
  _: '_',
  '{': '{',
  '}': '}',
  '\\': ' ',
  ',': ' ',
  ';': ' ',
  ':': ' ',
  '!': '',
  ' ': ' ',
};

// latexToText reduces one formula to the text it stands for.
function latexToText(formula: string): string {
  let text = formula;

  // A fraction is a slash between its parts. It has to be reduced before the
  // generic unwrapping, which would otherwise leave its braces behind.
  text = text.replace(/\\(?:d|t)?frac\s*\{([^{}]*)\}\s*\{([^{}]*)\}/g, (_match, top: string, bottom: string) => top + '/' + bottom);

  // A root keeps a marker, since its argument is what it applies to.
  text = text.replace(/\\sqrt\s*\{([^{}]*)\}/g, (_match, body: string) => '√' + body);

  // The wrappers nest — \boxed{29\text{ 个}} — so the innermost argument comes
  // out first and each pass exposes the next. The loop is bounded because a
  // malformed formula must not spin.
  for (let pass = 0; pass < 16; pass += 1) {
    const next = text.replace(/\\([a-zA-Z]+)\s*\{([^{}]*)\}/g, (_match, name: string, body: string) => {
      // A wrapper contributes its argument; a named limit keeps its name, so
      // that nothing is silently dropped whichever command shows up next.
      return LATEX_WRAPPERS.has(name) ? body : name + body;
    });

    if (next === text) break;
    text = next;
  }

  // Commands that take no argument, or whose argument has already been folded
  // in by the loop above.
  text = text
    .replace(/\\([a-zA-Z]+)/g, (_match, name: string) => {
      if (LATEX_IGNORED.has(name)) return ' ';
      return LATEX_SYMBOLS[name] ?? name;
    })
    .replace(/\\([^a-zA-Z\s])/g, (match) => LATEX_SYMBOLS[match.slice(1)] ?? match.slice(1));

  // Grouping braces have done their job by now; what is left of them is noise.
  text = text.replace(/[{}]/g, '');

  // Collapse the runs the removals and substitutions leave behind.
  return text.replace(/[ \t]{2,}/g, ' ').trim();
}

export function normalizeCandyAnswerMarkdown(answer: string): string {
  // Display math stands on its own: the model wrote it as a statement, and
  // leaving it inline would run it into the sentence before it.
  const withDisplayMath = answer
    .replace(/\$\$([\s\S]*?)\$\$/g, (_match, body: string) => displayMathParagraph(body))
    .replace(/\\\[([\s\S]*?)\\\]/g, (_match, body: string) => displayMathParagraph(body));

  const withInlineMath = withDisplayMath.replace(/\\\(([\s\S]*?)\\\)/g, (_match, body: string) => latexToText(body));

  // A display formula already arrives padded with blank lines, and the padding
  // it is given here adds more; three or more newlines in a row read as a hole
  // in the answer rather than as paragraph spacing.
  return withInlineMath.replace(/\n{3,}/g, '\n\n');
}

// displayMathParagraph keeps a display formula on a paragraph of its own, with
// the blank lines markdown needs to start one.
function displayMathParagraph(body: string): string {
  return '\n\n' + latexToText(body) + '\n\n';
}
