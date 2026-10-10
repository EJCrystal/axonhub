'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconChevronDown, IconChevronRight, IconLoader2 } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { ErrorDisplay } from '@/features/channels/utils/error-formatter';
import { IntelligenceHTMLPreview } from './intelligence-html-preview';
import { CandyAnswer } from './intelligence-candy-answer';
import { formatDuration } from '../data/format-duration';
import {
  canRecordManualVerdict,
  intelligenceVerdict,
  isRunInFlight,
  runIntelligenceVerdict,
  verdictBadgeClass,
  benchmarkBadgeClass,
} from './intelligence-verdict';
import { IntelligenceRun, IntelligenceRunConnection } from '../data/schema';
import { useSetIntelligenceRunVerdict } from '../data/intelligence';

// The API key a run exercised, rendered the way the rest of the page masks
// secrets. A run normally targets one key; when it covers several, joining the
// prefixes keeps every one of them visible rather than showing only the first.
function runKeyLabel(run: IntelligenceRun): string {
  const prefixes = run.results.map((result) => result.keyPrefix).filter((prefix) => prefix.trim() !== '');

  return prefixes.length > 0 ? prefixes.join(', ') : '—';
}

// runBenchmark names the check a run used. Runs recorded before the field
// existed carry no value; a candy answer is still recognisable from its result,
// so those rows are not mislabelled as pelican.
function runBenchmark(run: IntelligenceRun): string {
  if (run.benchmark === 'candy') return 'candy';
  if (run.benchmark) return run.benchmark;

  return run.results.some((result) => result.answer.trim() !== '') ? 'candy' : 'pelican';
}

// formatRunTime renders a run's start time compactly.
//
// toLocaleString spends a lot of width on seconds and a locale-specific date,
// which the time column does not need: the list is always newest first and about
// the same few hours, so the date is kept short and the time to the minute.
function formatRunTime(createdAt: string): string {
  const at = new Date(createdAt);
  if (Number.isNaN(at.getTime())) return createdAt;

  const pad = (value: number) => String(value).padStart(2, '0');

  return `${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`;
}

// benchmarkLabel names the check a run used, for the history column.
function benchmarkLabel(t: (key: string) => string, benchmark: string): string {
  return benchmark === 'candy'
    ? t('intelligence.settings.benchmarkCandy')
    : t('intelligence.settings.benchmarkPelican');
}

interface Props {
  history?: IntelligenceRunConnection;
  loading: boolean;
  configured: boolean;
}

export function IntelligenceHistoryList({ history, loading, configured }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const setVerdict = useSetIntelligenceRunVerdict();

  if (!configured) {
    return <p className='text-muted-foreground py-12 text-center text-sm'>{t('intelligence.history.notConfigured')}</p>;
  }

  const runs = history?.edges.map((edge) => edge.node) ?? [];

  // While the first load is in flight there is nothing to lay out yet, and
  // rendering the table header only to replace it with the empty state makes
  // the page flicker. Show a spinner until the data settles, then either the
  // rows or the empty state.
  if (loading && runs.length === 0) {
    return (
      <div className='text-muted-foreground flex items-center justify-center gap-2 py-12 text-sm'>
        <IconLoader2 className='h-4 w-4 animate-spin' />
        {t('intelligence.history.loading')}
      </div>
    );
  }

  if (runs.length === 0) {
    return <p className='text-muted-foreground py-12 text-center text-sm'>{t('intelligence.history.empty')}</p>;
  }

  return (
    // Tighter cells and a compact header: the table is mostly short values, and the
    // default row height pushed the records off screen.
    // The table has eight columns of short values; on a narrow window it scrolls
    // sideways rather than squeezing the text into unreadable columns.
    <div className='overflow-x-auto rounded-lg border [&_td]:px-2 [&_td]:py-1 [&_th]:h-8 [&_th]:px-2'>
      <Table className='min-w-[720px]'>
        <TableHeader>
          <TableRow>
            <TableHead className='w-10'></TableHead>
            <TableHead>{t('intelligence.history.columns.time')}</TableHead>
            <TableHead>{t('intelligence.history.columns.model')}</TableHead>
            <TableHead>{t('intelligence.history.columns.benchmark')}</TableHead>
            <TableHead>{t('intelligence.history.columns.apiKey')}</TableHead>
            <TableHead>{t('intelligence.history.columns.trigger')}</TableHead>
            <TableHead>{t('intelligence.history.columns.result')}</TableHead>
            <TableHead>{t('intelligence.history.columns.duration')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((run) => {
            const isOpen = expanded[run.id] ?? false;
            const inFlight = isRunInFlight(run);
            const verdict = runIntelligenceVerdict(run);
            return (
              <>
                <TableRow
                  key={run.id}
                  className='cursor-pointer'
                  onClick={() => setExpanded((prev) => ({ ...prev, [run.id]: !isOpen }))}
                  aria-expanded={isOpen}
                >
                  <TableCell>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='h-6 w-6'
                      onClick={(event) => {
                        // The row itself toggles; stop the click from firing twice.
                        event.stopPropagation();
                        setExpanded((prev) => ({ ...prev, [run.id]: !isOpen }));
                      }}
                      aria-label={t('intelligence.history.toggle')}
                    >
                      {isOpen ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />}
                    </Button>
                  </TableCell>
                  <TableCell className='text-xs tabular-nums'>{formatRunTime(run.createdAt)}</TableCell>
                  <TableCell className='font-mono text-xs leading-tight'>
                    {run.modelID}
                    {/* The level belongs next to the model: the same model at a
                        different level is a different check. */}
                    {run.reasoningEffort && (
                      <span className='text-muted-foreground ml-1'>({run.reasoningEffort})</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {/* A badge rather than plain text: the column is scanned, not
                        read, and the two checks deserve to be told apart at a
                        glance. */}
                    <Badge
                      variant='outline'
                      className={cn('h-5 px-1.5 text-[11px]', benchmarkBadgeClass(runBenchmark(run)))}
                    >
                      {benchmarkLabel(t, runBenchmark(run))}
                    </Badge>
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {/* A run targets one key, so the row shows it directly; a run
                        covering several keys lists them instead of guessing. */}
                    {runKeyLabel(run)}
                  </TableCell>
                  <TableCell className='text-xs'>{t(`intelligence.trigger.${run.trigger}`)}</TableCell>
                  <TableCell>
                    <div className='flex items-center gap-2'>
                      {inFlight ? (
                        <Badge variant='outline' className='h-5 border-sky-200 bg-sky-50 px-1.5 text-[11px] text-sky-700 dark:border-sky-800 dark:bg-sky-950 dark:text-sky-300'>
                          <IconLoader2 className='mr-1 h-3 w-3 animate-spin' />
                          {t('intelligence.history.running')}
                        </Badge>
                      ) : (
                        <>
                          <Badge variant='outline' className={cn('h-5 px-1.5 text-[11px]', verdictBadgeClass(verdict))}>
                            {t(`channels.dialogs.intelligence.verdict.${verdict}`)}
                          </Badge>
                          <span className='text-muted-foreground text-[11px] tabular-nums'>
                            {t('intelligence.history.keysSummary', {
                              success: run.successKeys,
                              total: run.totalKeys,
                            })}
                          </span>
                        </>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className='text-xs'>{inFlight ? '—' : formatDuration(run.durationMs)}</TableCell>
                </TableRow>

                {isOpen && (
                  <TableRow key={`${run.id}-detail`}>
                    <TableCell colSpan={8} className='bg-muted/30'>
                      <div className='space-y-3 py-2'>
                        {run.results.map((key) => (
                          <div key={key.keyPrefix} className='rounded-md border p-3 text-xs'>
                            <div className='flex flex-wrap items-center gap-2'>
                              <span className='font-mono'>{key.keyPrefix}</span>
                              {/* A run in flight has no outcome yet. Its seeded
                                  key entry would otherwise read as a plain
                                  failure, claiming a verdict that has not
                                  happened. */}
                              {inFlight ? (
                                <Badge
                                  variant='outline'
                                  className='border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-800 dark:bg-sky-950 dark:text-sky-300'
                                >
                                  <IconLoader2 className='mr-1 h-3 w-3 animate-spin' />
                                  {t('intelligence.history.running')}
                                </Badge>
                              ) : (
                                <Badge variant='outline' className={verdictBadgeClass(intelligenceVerdict(key))}>
                                  {t(`channels.dialogs.intelligence.verdict.${intelligenceVerdict(key)}`)}
                                </Badge>
                              )}
                              {key.manualVerdict && (
                                <span className='text-muted-foreground'>
                                  {t('intelligence.history.manual.label', {
                                    verdict: t(`channels.dialogs.intelligence.verdict.${key.manualVerdict}`),
                                  })}
                                </span>
                              )}
                              {key.taskID && (
                                <span className='text-muted-foreground'>
                                  {t('channels.dialogs.intelligence.taskID', { id: key.taskID })}
                                </span>
                              )}
                            </div>
                            {key.reason && <p className='mt-2'>{key.reason}</p>}
                            {key.durationMs > 0 && (
                              <p className='text-muted-foreground mt-2'>
                                {t('intelligence.history.durationLabel')} {formatDuration(key.durationMs)}
                                {key.generationMs > 0 && ` · ${t('intelligence.history.generationLabel')} ${formatDuration(key.generationMs)}`}
                              </p>
                            )}
                            {key.error && <ErrorDisplay error={key.error} messageClassName='mt-2 text-xs text-red-600' />}
                            {/* A run can fail after the model already produced its answer. The
                                reader then has to judge the source themselves, so the call to
                                action comes first and the cause follows. Only the inconclusive
                                case gets this: a run that generated nothing is a plain failure
                                with no source to review. */}
                            {!inFlight && intelligenceVerdict(key) === 'inconclusive' && (
                              <div className='mt-2 rounded-md border border-dashed p-2'>
                                <p className='font-medium'>{t('intelligence.history.previewAfterFailure')}</p>
                                <p className='text-muted-foreground mt-1'>
                                  {t('intelligence.history.previewAfterFailureDetail')}
                                </p>
                              </div>
                            )}
                            {/* Automatic scoring can leave no usable verdict; a human then
                                has to judge the source and record the decision here. A run
                                that failed before any page was generated has nothing to
                                review, so the buttons stay hidden for it, as does a run
                                still in flight. */}
                            {!inFlight && canRecordManualVerdict(key) && (
                              <div className='mt-2 flex flex-wrap items-center gap-2'>
                                <Button
                                  variant='outline'
                                  size='sm'
                                  className='h-7'
                                  disabled={setVerdict.isPending}
                                  onClick={() =>
                                    setVerdict.mutate({ runID: run.id, keyPrefix: key.keyPrefix, verdict: 'degraded' })
                                  }
                                >
                                  {t('intelligence.history.manual.markDegraded')}
                                </Button>
                                <Button
                                  variant='outline'
                                  size='sm'
                                  className='h-7'
                                  disabled={setVerdict.isPending}
                                  onClick={() =>
                                    setVerdict.mutate({ runID: run.id, keyPrefix: key.keyPrefix, verdict: 'normal' })
                                  }
                                >
                                  {t('intelligence.history.manual.markNormal')}
                                </Button>
                              </div>
                            )}
                            {key.manualVerdict && (
                              <div className='mt-2'>
                                <Button
                                  variant='ghost'
                                  size='sm'
                                  className='h-7'
                                  disabled={setVerdict.isPending}
                                  onClick={() => setVerdict.mutate({ runID: run.id, keyPrefix: key.keyPrefix })}
                                >
                                  {t('intelligence.history.manual.clear')}
                                </Button>
                              </div>
                            )}
                            {/* Nothing has been produced yet while the run is in
                                flight, so neither rendering would say anything true. */}
                            {!inFlight &&
                              (runBenchmark(run) === 'candy' ? (
                                <CandyAnswer answer={key.answer || key.html} />
                              ) : (
                                <IntelligenceHTMLPreview html={key.html} verdict={intelligenceVerdict(key)} compact />
                              ))}
                          </div>
                        ))}
                      </div>
                    </TableCell>
                  </TableRow>
                )}
              </>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
