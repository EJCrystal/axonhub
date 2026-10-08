// IntelligenceVerdict is the UI-facing summary of a run or one key.
//
// Everything collapses into three states, because that is what the reader acts
// on: the model looked normal, the model looked degraded, or the check never
// produced a usable answer. A scoring failure and an inconclusive classifier
// both land in "failed" — from the operator's side they are the same thing,
// no automatic verdict — and the detail line says which one it was.
export type IntelligenceVerdict = 'normal' | 'degraded' | 'failed';

export interface VerdictInput {
  success: boolean;
  quality: string;
  // Operator's decision, which wins over whatever scoring reported.
  manualVerdict?: string | null;
}

export function intelligenceVerdict(result: VerdictInput): IntelligenceVerdict {
  switch (result.manualVerdict) {
    case 'normal':
      return 'normal';
    case 'degraded':
      return 'degraded';
  }

  if (!result.success) return 'failed';

  switch (result.quality) {
    case 'normal':
      return 'normal';
    case 'degraded':
    case 'suspicious':
      return 'degraded';
    default:
      return 'failed';
  }
}

// runIntelligenceVerdict collapses one run's per-key outcomes into the headline
// the history table shows. The stored run status only tracks whether every key
// produced an answer, so a run whose keys all came back degraded would still
// read as succeeded; the verdict is what says whether the model actually
// passed, and the two must not be conflated.
//
// It reads the per-key verdicts rather than the stored success counter, because
// a manual verdict has to be able to move a run that automatic scoring left
// with zero successes.
export function runIntelligenceVerdict(run: { results?: VerdictInput[] }): IntelligenceVerdict {
  const results = run.results ?? [];
  if (results.length === 0) return 'failed';

  const verdicts = results.map(intelligenceVerdict);
  if (verdicts.includes('degraded')) return 'degraded';
  if (verdicts.every((verdict) => verdict === 'normal')) return 'normal';

  return 'failed';
}

export function verdictBadgeVariant(verdict: IntelligenceVerdict): 'default' | 'destructive' | 'secondary' {
  switch (verdict) {
    case 'normal':
      return 'default';
    case 'degraded':
      return 'destructive';
    default:
      return 'secondary';
  }
}

// verdictBadgeClass colours the badge by outcome: green when the model looked
// normal, amber when it looked degraded, red when the check never produced a
// usable answer. The variants above stay for callers that only need the
// semantic name; this is what the history table paints with.
export function verdictBadgeClass(verdict: IntelligenceVerdict): string {
  switch (verdict) {
    case 'normal':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-400';
    case 'degraded':
      return 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-400';
    default:
      return 'border-red-200 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-400';
  }
}

// canRecordManualVerdict reports whether an operator can still decide a key's
// outcome by hand.
//
// That only makes sense once the model actually produced a page: a run that
// failed before generation has no source to review, so offering the buttons
// would invite a judgement nobody is able to make. The buttons are for the
// narrower case where the page exists but automatic scoring could not settle
// it, so the source is there for a human to read.
export function canRecordManualVerdict(result: VerdictInput & { html?: string | null }): boolean {
  if (result.manualVerdict) return false;
  if (!result.html || result.html.trim() === '') return false;

  return intelligenceVerdict(result) === 'failed';
}
