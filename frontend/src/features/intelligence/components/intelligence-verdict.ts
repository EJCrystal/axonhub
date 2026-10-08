// IntelligenceVerdict is the UI-facing summary of a run or one key.
//
// Four states, because that is what the reader acts on:
//
//   normal        the model looked normal
//   degraded      the model looked degraded
//   inconclusive  a page was produced, but automatic scoring could not settle
//                 it — a human has to read the source and decide
//   failed        no page was produced at all, so there is nothing to judge
//
// The split between the last two is the whole point: a check that never
// generated anything is a failure, whereas one that generated a page but could
// not score it is a question for a person. Collapsing them would either hide a
// real failure or invite a decision nobody can make.
export type IntelligenceVerdict = 'normal' | 'degraded' | 'inconclusive' | 'failed';

export interface VerdictInput {
  success: boolean;
  quality: string;
  // The source the model produced. Absent when generation never happened (or
  // when the document was too large to ship), which is what tells a failure
  // apart from a scoring result nobody could settle.
  html?: string | null;
  // Operator's decision, which wins over whatever scoring reported.
  manualVerdict?: string | null;
}

// hasGeneratedHTML reports whether the model produced a page worth reviewing.
// The backend omits html when generation never happened, so its absence is what
// separates an outright failure from a result nobody could settle.
function hasGeneratedHTML(html?: string | null): boolean {
  return typeof html === 'string' && html.trim() !== '';
}

export function intelligenceVerdict(result: VerdictInput): IntelligenceVerdict {
  switch (result.manualVerdict) {
    case 'normal':
      return 'normal';
    case 'degraded':
      return 'degraded';
  }

  // A decisive automatic verdict stands on its own. It is checked before the
  // source so an oversized document — whose html the backend drops — still
  // reports what the scoring actually said.
  if (result.success) {
    switch (result.quality) {
      case 'normal':
        return 'normal';
      case 'degraded':
      case 'suspicious':
        return 'degraded';
    }
  }

  // No automatic verdict. With a source in hand a human can still decide; with
  // nothing generated the check simply failed.
  return hasGeneratedHTML(result.html) ? 'inconclusive' : 'failed';
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

  // Ranking, worst first. A degraded key decides the run over anything milder,
  // and a run with nothing generated at all reads as a plain failure rather
  // than a question for a human.
  for (const verdict of ['degraded', 'inconclusive', 'failed'] as const) {
    if (verdicts.includes(verdict)) return verdict;
  }

  return 'normal';
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
// normal, amber when it looked degraded, red when nothing was generated, and a
// muted slate for the in-between case that needs a human. The variants above
// stay for callers that only need the semantic name; this is what the history
// table paints with.
export function verdictBadgeClass(verdict: IntelligenceVerdict): string {
  switch (verdict) {
    case 'normal':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-400';
    case 'degraded':
      return 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-400';
    case 'inconclusive':
      return 'border-slate-300 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300';
    default:
      return 'border-red-200 bg-red-50 text-red-700 dark:border-red-800 dark:bg-red-950 dark:text-red-400';
  }
}

// canRecordManualVerdict reports whether an operator can still decide a key's
// outcome by hand: exactly the inconclusive case, where a page exists but
// automatic scoring could not settle it.
export function canRecordManualVerdict(result: VerdictInput): boolean {
  if (result.manualVerdict) return false;

  return intelligenceVerdict(result) === 'inconclusive';
}

// isRunInFlight reports whether a run has not finished yet. The verdict helpers
// describe outcomes, which a run in flight does not have — it must be shown as
// in progress rather than folded into "failed", which would claim a result that
// has not happened.
export function isRunInFlight(run: { status?: string }): boolean {
  return run.status === 'running';
}
