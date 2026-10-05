'use client';

import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconBrain, IconRefresh } from '@tabler/icons-react';
import { Main } from '@/components/layout/main';
import { PageHeader } from '@/components/layout/page-header';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { usePermissions } from '@/hooks/usePermissions';
import { IntelligenceSettings } from './components/intelligence-settings';
import { IntelligenceHistoryList } from './components/intelligence-history-list';
import { useQueryChannels } from '@/features/channels/data/channels';
import { useIntelligenceConfig, useIntelligenceHistory, useRunIntelligenceCheckNow } from './data/intelligence';

export default function IntelligenceManagement() {
  const { t } = useTranslation();
  const { hasSystemScope } = usePermissions();
  const { data: config, isLoading: configLoading } = useIntelligenceConfig();
  const runNow = useRunIntelligenceCheckNow();
  const canRun = hasSystemScope('write_channels');

  const [channelID, setChannelID] = useState<string>();

  // The filter lists every channel, not just the configured ones: a channel
  // that was removed from the configuration still has history worth reading.
  const { data: channels } = useQueryChannels({ first: 200 });
  const channelOptions = useMemo(
    () => (channels?.edges ?? []).map((edge) => ({ id: edge.node.id, name: edge.node.name })),
    [channels]
  );

  // Default the history view to the first configured channel so the page has
  // content without an extra click.
  const effectiveChannelID = useMemo(() => channelID ?? config?.targets[0]?.channelID, [channelID, config?.targets]);

  const { data: history, isLoading: historyLoading, refetch, isFetching } = useIntelligenceHistory(effectiveChannelID);

  const actions = (
    <div className='flex items-center gap-2'>
      {channelOptions.length > 0 && (
        <Select value={effectiveChannelID} onValueChange={setChannelID}>
          <SelectTrigger className='w-48'>
            <SelectValue placeholder={t('intelligence.history.channelPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            {channelOptions.map((channel) => (
              <SelectItem key={channel.id} value={channel.id}>
                {channel.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Button variant='outline' size='sm' onClick={() => refetch()} disabled={isFetching}>
        <IconRefresh className='mr-1 h-4 w-4' />
        {t('intelligence.history.refresh')}
      </Button>
      <Button
        size='sm'
        onClick={() => runNow.mutate()}
        disabled={!canRun || runNow.isPending || (config?.targets.length ?? 0) === 0}
        data-testid='run-intelligence-now'
      >
        <IconBrain className='mr-1 h-4 w-4' />
        {runNow.isPending ? t('intelligence.running') : t('intelligence.runNow')}
      </Button>
    </div>
  );

  return (
    <Main>
      <PageHeader title={t('intelligence.title')} description={t('intelligence.description')} actions={actions} />

      <Tabs defaultValue='history' className='space-y-4'>
        <TabsList>
          <TabsTrigger value='history'>{t('intelligence.tabs.history')}</TabsTrigger>
          <TabsTrigger value='settings'>{t('intelligence.tabs.settings')}</TabsTrigger>
        </TabsList>

        <TabsContent value='history'>
          <IntelligenceHistoryList
            history={history}
            loading={historyLoading}
            configured={(config?.targets.length ?? 0) > 0}
          />
        </TabsContent>

        <TabsContent value='settings'>
          <IntelligenceSettings config={config} loading={configLoading} readOnly={!canRun} />
        </TabsContent>
      </Tabs>
    </Main>
  );
}
