import { IntelligenceRun } from './schema';
import { IntelligenceVerdict, intelligenceVerdict, runIntelligenceVerdict } from '../components/intelligence-verdict';

// The verdict filter offers "all" plus the three verdicts, so a reader can ask
// for just the degraded runs or just the ones automatic scoring could not
// decide.
export type VerdictFilter = 'all' | IntelligenceVerdict;

export interface HistoryFilter {
  verdict: VerdictFilter;
  // Run-level model, from the record itself.
  modelID?: string;
  // Masked key prefix, matched against the keys a run evaluated.
  keyPrefix?: string;
}

export const DEFAULT_HISTORY_FILTER: HistoryFilter = { verdict: 'all' };

export function isHistoryFilterActive(filter: HistoryFilter): boolean {
  return filter.verdict !== 'all' || Boolean(filter.modelID) || Boolean(filter.keyPrefix);
}

// historyFilterOptions lists what the loaded runs actually contain, so the
// pickers never offer a model or key that has no records behind it.
export function historyFilterOptions(runs: IntelligenceRun[]): { models: string[]; keys: string[] } {
  const models = new Set<string>();
  const keys = new Set<string>();

  for (const run of runs) {
    if (run.modelID) models.add(run.modelID);
    for (const result of run.results) {
      if (result.keyPrefix) keys.add(result.keyPrefix);
    }
  }

  return { models: [...models].sort(), keys: [...keys].sort() };
}

// narrowToKey answers "what did this one key do" by dropping the run's other
// keys, then recomputing the counters so the row badge describes what is
// actually on screen rather than the whole run.
function narrowToKey(run: IntelligenceRun, keyPrefix: string): IntelligenceRun | null {
  const results = run.results.filter((result) => result.keyPrefix === keyPrefix);
  if (results.length === 0) return null;

  const successKeys = results.filter((result) => intelligenceVerdict(result) === 'normal').length;

  return {
    ...run,
    results,
    totalKeys: results.length,
    successKeys,
    failedKeys: results.length - successKeys,
  };
}

// filterRuns applies the history filter to a list of runs, newest first order
// preserved. A run matches the verdict filter when its own (possibly narrowed)
// verdict matches, so filtering by key reports that key's outcome rather than
// the whole run's.
export function filterRuns(runs: IntelligenceRun[], filter: HistoryFilter): IntelligenceRun[] {
  const filtered: IntelligenceRun[] = [];

  for (const run of runs) {
    let candidate = run;

    if (filter.keyPrefix) {
      const narrowed = narrowToKey(run, filter.keyPrefix);
      if (!narrowed) continue;
      candidate = narrowed;
    }

    if (filter.modelID && candidate.modelID !== filter.modelID) continue;
    if (filter.verdict !== 'all' && runIntelligenceVerdict(candidate) !== filter.verdict) continue;

    filtered.push(candidate);
  }

  return filtered;
}
