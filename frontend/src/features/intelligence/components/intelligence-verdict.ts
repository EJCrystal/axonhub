// IntelligenceVerdict is the UI-facing summary of a manxue.ai assessment.
//
// The service reports `normal`, `degraded`, `unknown` (and historically
// `suspicious`), plus whatever the run itself failed with. Those collapse into
// four states so the badge never claims more than the service did: `unknown`
// means the classifier could not tell, which is a different verdict from
// `degraded`, and an incomplete run produced no verdict at all.
export type IntelligenceVerdict = 'normal' | 'degraded' | 'unknown' | 'incomplete';

export function intelligenceVerdict(result: { success: boolean; quality: string }): IntelligenceVerdict {
  if (!result.success) return 'incomplete';

  switch (result.quality) {
    case 'normal':
      return 'normal';
    case 'degraded':
    case 'suspicious':
      return 'degraded';
    default:
      return 'unknown';
  }
}

// runIntelligenceVerdict collapses one run's per-key outcomes into the headline
// the history table shows. The stored run status only tracks whether every key
// produced an answer, so a run whose keys all came back "degraded" would still
// read as succeeded; the verdict is what says whether the model actually
// passed, and the two must not be conflated.
export function runIntelligenceVerdict(run: {
  successKeys: number;
  results?: { success: boolean; quality: string }[];
}): IntelligenceVerdict {
  const results = run.results ?? [];
  if (run.successKeys === 0 || results.length === 0) return 'incomplete';

  const verdicts = results.map(intelligenceVerdict);
  if (verdicts.includes('degraded')) return 'degraded';
  if (verdicts.every((verdict) => verdict === 'normal')) return 'normal';

  return 'unknown';
}

export function verdictBadgeVariant(verdict: IntelligenceVerdict): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (verdict) {
    case 'normal':
      return 'default';
    case 'degraded':
      return 'destructive';
    case 'unknown':
      return 'secondary';
    default:
      return 'outline';
  }
}
