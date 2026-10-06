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
import {
  useAllIntelligenceRuns,
  useIntelligenceConfig,
  useIntelligenceHistory,
  useRunIntelligenceCheckNow,
} from './data/intelligence';
import { IntelligenceRun, IntelligenceRunConnection } from './data/schema';

// Sentinel for the “all channels” choice. Radix Select reserves the empty
// string, so the unfiltered option needs a non-empty value.
const ALL_CHANNELS = '__all__';

interface ChannelRunGroup {
  channelID: number;
  channelName: string;
  edges: { node: IntelligenceRun; cursor: string }[];
}

// toConnection wraps one channel’s edges back into the connection shape the
// history table expects, so the same component renders both views.
function toConnection(edges: { node: IntelligenceRun; cursor: string }[]): IntelligenceRunConnection {
  return {
    edges,
    totalCount: edges.length,
    pageInfo: { hasNextPage: false, hasPreviousPage: false, startCursor: null, endCursor: null },
  };
}

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

  const allRuns = useAllIntelligenceRuns(200);
  const historyQuery = useIntelligenceHistory(channelID);

  // Without a channel filter the page merges every channel’s runs, keeping
  // the newest-first order and grouping consecutive runs by their channel.
  const groups = useMemo<ChannelRunGroup[]>(() => {
    const order: number[] = [];
    const byChannel = new Map<number, ChannelRunGroup>();

    for (const edge of allRuns.data?.edges ?? []) {
      const node = edge.node;
      let group = byChannel.get(node.channelID);
      if (!group) {
        group = { channelID: node.channelID, channelName: node.channelName, edges: [] };
        byChannel.set(node.channelID, group);
        order.push(node.channelID);
      }
      group.edges.push(edge);
    }

    return order.map((id) => byChannel.get(id)!).filter(Boolean);
  }, [allRuns.data]);

  const showingAll = !channelID;
  const loading = showingAll ? allRuns.isLoading : historyQuery.isLoading;
  const fetching = showingAll ? allRuns.isFetching : historyQuery.isFetching;
  const configured = (config?.targets.length ?? 0) > 0;

  const refresh = () => {
    if (showingAll) {
      void allRuns.refetch();
    } else {
      void historyQuery.refetch();
    }
  };

  const actions = (
    <div className='flex items-center gap-2'>
      {channelOptions.length > 0 && (
        <Select value={channelID ?? ALL_CHANNELS} onValueChange={(value) => setChannelID(value === ALL_CHANNELS ? undefined : value)}>
          <SelectTrigger className='w-48'>
            <SelectValue placeholder={t('intelligence.history.channelPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_CHANNELS}>{t('intelligence.history.allChannels')}</SelectItem>
            {channelOptions.map((channel) => (
              <SelectItem key={channel.id} value={channel.id}>
                {channel.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Button variant='outline' size='sm' onClick={refresh} disabled={fetching}>
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
          {showingAll && groups.length > 0 ? (
            <div className='space-y-6'>
              {groups.map((group) => (
                <div key={group.channelID} className='space-y-2'>
                  <div className='flex items-center gap-2'>
                    <h3 className='text-sm font-medium'>{group.channelName || '#' + group.channelID}</h3>
                    <span className='text-muted-foreground text-xs'>
                      {t('intelligence.history.groupCount', { count: group.edges.length })}
                    </span>
                  </div>
                  <IntelligenceHistoryList history={toConnection(group.edges)} loading={false} configured={configured} />
                </div>
              ))}
            </div>
          ) : (
            <IntelligenceHistoryList
              history={showingAll ? undefined : historyQuery.data}
              loading={loading}
              configured={configured}
            />
          )}
        </TabsContent>

        <TabsContent value='settings'>
          <IntelligenceSettings config={config} loading={configLoading} readOnly={!canRun} />
        </TabsContent>
      </Tabs>
    </Main>
  );
}
