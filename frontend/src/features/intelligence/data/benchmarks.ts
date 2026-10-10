import { REASONING_EFFORTS } from '@/features/models/data/reasoning-efforts';

// The published checks a target may run, in the order the pickers list them.
// The first entry is the default, which is why the empty stored value means
// pelican: every target recorded before the choice existed ran it.
export const INTELLIGENCE_BENCHMARKS = ['pelican', 'candy'] as const;
export type IntelligenceBenchmark = (typeof INTELLIGENCE_BENCHMARKS)[number];

// The thinking levels the pickers offer, plus the empty value that means "use
// the provider default".
export const REASONING_EFFORT_OPTIONS = REASONING_EFFORTS;

export function benchmarkLabelKey(benchmark: string): string {
  return benchmark === 'candy'
    ? 'intelligence.settings.benchmarkCandy'
    : 'intelligence.settings.benchmarkPelican';
}
