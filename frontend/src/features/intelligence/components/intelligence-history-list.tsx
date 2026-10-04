'use client';

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconChevronDown, IconChevronRight } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { ErrorDisplay } from '@/features/channels/utils/error-formatter';
import { IntelligenceHTMLPreview } from '@/features/channels/components/channels-intelligence-preview';
import { intelligenceVerdict, verdictBadgeVariant } from '@/features/channels/components/intelligence-verdict';
import { IntelligenceRun, IntelligenceRunConnection } from '../data/schema';

interface Props {
  history?: IntelligenceRunConnection;
  loading: boolean;
  configured: boolean;
}

function runBadgeVariant(status: IntelligenceRun['status']): 'default' | 'destructive' | 'secondary' {
  switch (status) {
    case 'succeeded':
      return 'default';
    case 'partial':
      return 'secondary';
    default:
      return 'destructive';
  }
}

export function IntelligenceHistoryList({ history, loading, configured }: Props) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  if (!configured) {
    return <p className='text-muted-foreground py-12 text-center text-sm'>{t('intelligence.history.notConfigured')}</p>;
  }

  const runs = history?.edges.map((edge) => edge.node) ?? [];

  if (!loading && runs.length === 0) {
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
                      <Badge variant={runBadgeVariant(run.status)}>{t(`intelligence.status.${run.status}`)}</Badge>
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
                              {key.taskId && (
                                <span className='text-muted-foreground'>
                                  {t('channels.dialogs.intelligence.taskID', { id: key.taskId })}
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
                            <IntelligenceHTMLPreview html={key.html} verdict={intelligenceVerdict(key)} />
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
