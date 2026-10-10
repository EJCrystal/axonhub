// formatDuration renders a millisecond duration for the history table.
//
// Under a minute the seconds are exact enough to compare runs at a glance
// ("46.8s"), but a slow channel runs for many minutes and "900.0s" is hard to
// read, so anything past a minute is shown as minutes and seconds ("15m 0s").
export function formatDuration(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs <= 0) return '—';

  const totalSeconds = durationMs / 1000;
  if (totalSeconds < 60) return totalSeconds.toFixed(1) + 's';

  const minutes = Math.floor(totalSeconds / 60);
  const seconds = Math.round(totalSeconds - minutes * 60);
  // Rounding can land on 60; carry it so the value never reads "1m 60s".
  if (seconds === 60) return minutes + 1 + 'm 0s';

  return minutes + 'm ' + seconds + 's';
}
