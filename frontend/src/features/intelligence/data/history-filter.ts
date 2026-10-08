import { IntelligenceRun } from './schema';
import { IntelligenceVerdict, intelligenceVerdict, runIntelligenceVerdict } from '../components/intelligence-verdict';

// The verdict filter offers "all" plus every verdict, so a reader can ask for
// just the degraded runs or just the ones automatic scoring could not decide.
export type VerdictFilter = 'all' | IntelligenceVerdict;

export interface HistoryFilter {
  verdict: VerdictFilter;
  // Run-level model, from the record itself.
  modelID?: string;
  // Masked key prefix, matched against the keys a run evaluated.
  keyPrefix?: string;
  // Thinking level the run requested. The empty string is its own choice: runs
  // recorded before the field existed, and those that used the provider default.
  reasoningEffort?: string;
}

export const DEFAULT_HISTORY_FILTER: HistoryFilter = { verdict: 'all' };

export function isHistoryFilterActive(filter: HistoryFilter): boolean {
  return (
    filter.verdict !== 'all' ||
    Boolean(filter.modelID) ||
    Boolean(filter.keyPrefix) ||
    filter.reasoningEffort !== undefined
  );
}

// historyFilterOptions lists what the loaded runs actually contain, so the
// pickers never offer a model or key that has no records behind it.
export function historyFilterOptions(runs: IntelligenceRun[]): {
  models: string[];
  keys: string[];
  efforts: string[];
} {
  const models = new Set<string>();
  const keys = new Set<string>();
  const efforts = new Set<string>();

  for (const run of runs) {
    if (run.modelID) models.add(run.modelID);
    if (run.reasoningEffort) efforts.add(run.reasoningEffort);
    for (const result of run.results) {
      if (result.keyPrefix) keys.add(result.keyPrefix);
    }
  }

  return { models: [...models].sort(), keys: [...keys].sort(), efforts: [...efforts].sort() };
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
    if (filter.reasoningEffort !== undefined && candidate.reasoningEffort !== filter.reasoningEffort) continue;
    if (filter.verdict !== 'all' && runIntelligenceVerdict(candidate) !== filter.verdict) continue;

    filtered.push(candidate);
  }

  return filtered;
}

// groupRunsByChannel collects runs per channel, keeping the newest-first order
// both across and within the groups. The merged history view stacks one tab per
// channel, so the grouping is what decides which channels appear and in what
// order.
export function groupRunsByChannel<T extends { channelID: number; channelName: string }>(
  runs: T[]
): { channelID: number; channelName: string; runs: T[] }[] {
  const order: number[] = [];
  const byChannel = new Map<number, { channelID: number; channelName: string; runs: T[] }>();

  for (const run of runs) {
    let group = byChannel.get(run.channelID);
    if (!group) {
      group = { channelID: run.channelID, channelName: run.channelName, runs: [] };
      byChannel.set(run.channelID, group);
      order.push(run.channelID);
    }
    group.runs.push(run);
  }

  return order.map((id) => byChannel.get(id)!);
}

// pickActiveChannel decides which channel tab is shown: the requested one when
// it still has records, otherwise the first one that does. A filter can remove
// the channel that was selected, and the view must not end up blank.
export function pickActiveChannel<T extends { channelID: number }>(
  groups: T[],
  requested: string
): T | undefined {
  if (groups.length === 0) return undefined;

  return groups.find((group) => String(group.channelID) === requested) ?? groups[0];
}
