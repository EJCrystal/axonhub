'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconChevronDown, IconChevronRight, IconLoader2 } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { ErrorDisplay } from '@/features/channels/utils/error-formatter';
import { IntelligenceHTMLPreview } from './intelligence-html-preview';
import {
  intelligenceVerdict,
  runIntelligenceVerdict,
  verdictBadgeVariant,
} from './intelligence-verdict';
import { IntelligenceRunConnection } from '../data/schema';

interface Props {
  history?: IntelligenceRunConnection;
  loading: boolean;
  configured: boolean;
}

export function IntelligenceHistoryList({ history, loading, configured }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

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
    <div className='rounded-lg border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className='w-10'></TableHead>
            <TableHead>{t('intelligence.history.columns.time')}</TableHead>
            <TableHead>{t('intelligence.history.columns.model')}</TableHead>
            <TableHead>{t('intelligence.history.columns.trigger')}</TableHead>
            <TableHead>{t('intelligence.history.columns.result')}</TableHead>
            <TableHead>{t('intelligence.history.columns.duration')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((run) => {
            const isOpen = expanded[run.id] ?? false;
            const verdict = runIntelligenceVerdict(run);
            return (
              <>
                <TableRow key={run.id}>
                  <TableCell>
                    <Button
                      variant='ghost'
                      size='icon'
                      className='h-6 w-6'
                      onClick={() => setExpanded((prev) => ({ ...prev, [run.id]: !isOpen }))}
                      aria-label={t('intelligence.history.toggle')}
                    >
                      {isOpen ? <IconChevronDown size={14} /> : <IconChevronRight size={14} />}
                    </Button>
                  </TableCell>
                  <TableCell className='text-xs'>{new Date(run.createdAt).toLocaleString()}</TableCell>
                  <TableCell className='font-mono text-xs'>{run.modelID}</TableCell>
                  <TableCell className='text-xs'>{t(`intelligence.trigger.${run.trigger}`)}</TableCell>
                  <TableCell>
                    <div className='flex items-center gap-2'>
                      <Badge variant={verdictBadgeVariant(verdict)}>
                        {t(`channels.dialogs.intelligence.verdict.${verdict}`)}
                      </Badge>
                      <span className='text-muted-foreground text-xs'>
                        {t('intelligence.history.keysSummary', {
                          success: run.successKeys,
                          total: run.totalKeys,
                        })}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className='text-xs'>{(run.durationMs / 1000).toFixed(1)}s</TableCell>
                </TableRow>

                {isOpen && (
                  <TableRow key={`${run.id}-detail`}>
                    <TableCell colSpan={6} className='bg-muted/30'>
                      <div className='space-y-3 py-2'>
                        {run.results.map((key) => (
                          <div key={key.keyPrefix} className='rounded-md border p-3 text-xs'>
                            <div className='flex flex-wrap items-center gap-2'>
                              <span className='font-mono'>{key.keyPrefix}</span>
                              <Badge variant={verdictBadgeVariant(intelligenceVerdict(key))}>
                                {t(`channels.dialogs.intelligence.verdict.${intelligenceVerdict(key)}`)}
                              </Badge>
                              {key.taskID && (
                                <span className='text-muted-foreground'>
                                  {t('channels.dialogs.intelligence.taskID', { id: key.taskID })}
                                </span>
                              )}
                            </div>
                            {key.reason && <p className='mt-2'>{key.reason}</p>}
                            {key.durationMs > 0 && (
                              <p className='text-muted-foreground mt-2'>
                                {t('channels.dialogs.intelligence.duration', { seconds: (key.durationMs / 1000).toFixed(1) })}
                                {key.generationMs > 0 &&
                                  ` · ${t('channels.dialogs.intelligence.generation', { seconds: (key.generationMs / 1000).toFixed(1) })}`}
                              </p>
                            )}
                            {key.error && <ErrorDisplay error={key.error} messageClassName='mt-2 text-xs text-red-600' />}
                            {/* A run can fail after the model already produced its answer, so a
                                failed key still has source worth looking at. Say why it is here. */}
                            {key.error && key.html && (
                              <p className='text-muted-foreground mt-2'>
                                {t('intelligence.history.previewAfterFailure')}
                              </p>
                            )}
                            <IntelligenceHTMLPreview html={key.html} verdict={intelligenceVerdict(key)} compact />
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
