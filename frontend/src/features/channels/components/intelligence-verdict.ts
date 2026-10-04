// IntelligenceVerdict is the UI-facing summary of a manxue.ai assessment.
//
// The service reports `normal`, `degraded`, `unknown` (and historically
// `suspicious`), plus whatever the run itself failed with. Those collapse into
// four states so the badge never claims more than the service did: `unknown`
// means the classifier could not tell, which is a different verdict from
// `degraded`, and a failed run produced no verdict at all.
export type IntelligenceVerdict = 'normal' | 'failed' | 'unknown' | 'incomplete';

export function intelligenceVerdict(result: { success: boolean; quality: string }): IntelligenceVerdict {
  if (!result.success) return 'incomplete';

  switch (result.quality) {
    case 'normal':
      return 'normal';
    case 'degraded':
    case 'suspicious':
      return 'failed';
    default:
      return 'unknown';
  }
}

export function verdictBadgeVariant(verdict: IntelligenceVerdict): 'default' | 'destructive' | 'secondary' | 'outline' {
  switch (verdict) {
    case 'normal':
      return 'default';
    case 'failed':
      return 'destructive';
    case 'unknown':
      return 'secondary';
    default:
      return 'outline';
  }
}
