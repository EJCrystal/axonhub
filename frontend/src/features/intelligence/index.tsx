'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconBrain, IconRefresh } from '@tabler/icons-react';
import { Main } from '@/components/layout/main';
import { PageHeader } from '@/components/layout/page-header';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { usePermissions } from '@/hooks/usePermissions';
import { useQueryChannels } from '@/features/channels/data/channels';
import { IntelligenceSettings } from './components/intelligence-settings';
import { IntelligenceHistoryList } from './components/intelligence-history-list';
import { modelsForAPIKey } from './data/api-key-models';
import { maskAPIKey } from './data/mask-key';
import {
  DEFAULT_HISTORY_FILTER,
  HistoryFilter,
  VerdictFilter,
  filterRuns,
  historyFilterOptions,
  isHistoryFilterActive,
} from './data/history-filter';
import {
  useAllIntelligenceRuns,
  useIntelligenceConfig,
  useIntelligenceHistory,
  useRunIntelligenceCheckNow,
} from './data/intelligence';
import { IntelligenceRun, IntelligenceRunConnection } from './data/schema';

// Sentinels for the "all ..." choices. Radix Select reserves the empty string,
// so the unfiltered option needs a non-empty value.
const ALL_CHANNELS = '__all__';
const ALL_KEYS = '__all_keys__';
const ALL_MODELS = '__all_models__';
const ALL_EFFORTS = '__all_efforts__';
// The effort picker needs a value for the provider default, which is the empty string.
const NO_EFFORT = '__no_effort__';

interface ChannelRunGroup {
  channelID: number;
  channelName: string;
  edges: { node: IntelligenceRun; cursor: string }[];
}

// toConnection wraps a run list back into the connection shape the history
// table expects, so the same component renders both views.
function toConnection(runs: IntelligenceRun[]): IntelligenceRunConnection {
  return {
    edges: runs.map((node) => ({ node, cursor: node.id })),
    totalCount: runs.length,
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
  const [verdict, setVerdict] = useState<VerdictFilter>('all');
  const [apiKey, setApiKey] = useState<string>();
  const [modelID, setModelID] = useState<string>();
  const [effort, setEffort] = useState<string>();

  // The filter lists every channel, not just the configured ones: a channel
  // that was removed from the configuration still has history worth reading.
  // The full option is required because the key picker reads the credentials.
  const { data: channels } = useQueryChannels({ first: 200, full: true });
  const channelOptions = useMemo(
    () => (channels?.edges ?? []).map((edge) => ({ id: edge.node.id, name: edge.node.name })),
    [channels]
  );

  const allRuns = useAllIntelligenceRuns(200);
  const historyQuery = useIntelligenceHistory(channelID);
  const showingAll = !channelID;

  // Switching channel invalidates the key and model narrowing, since both were
  // chosen against the previous channel's credentials.
  useEffect(() => {
    setApiKey(undefined);
    setModelID(undefined);
    setEffort(undefined);
  }, [channelID]);

  const selectedChannel = useMemo(
    () => (channels?.edges ?? []).find((edge) => edge.node.id === channelID)?.node,
    [channels, channelID]
  );

  // Keys come from the selected channel's own credentials. While every channel
  // is merged there is no single key list, so that picker is hidden and the
  // model picker falls back to whatever the loaded runs actually contain.
  const keyOptions = useMemo(() => {
    const credentials = selectedChannel?.credentials;
    if (!credentials) return [];
    const all = [...(credentials.apiKeys ?? [])];
    if (credentials.apiKey && !all.includes(credentials.apiKey)) {
      all.unshift(credentials.apiKey);
    }
    return all.filter((key) => key.trim().length > 0);
  }, [selectedChannel]);

  const rawRuns = useMemo(() => allRuns.data?.edges.map((edge) => edge.node) ?? [], [allRuns.data]);
  const singleChannelRuns = useMemo(
    () => historyQuery.data?.edges.map((edge) => edge.node) ?? [],
    [historyQuery.data]
  );

  const sourceRuns = showingAll ? rawRuns : singleChannelRuns;

  const modelSelectOptions = useMemo(() => {
    if (!selectedChannel) return historyFilterOptions(sourceRuns).models;
    if (!apiKey) return selectedChannel.supportedModels ?? [];
    return modelsForAPIKey(selectedChannel.credentials, selectedChannel.supportedModels ?? [], apiKey);
  }, [selectedChannel, apiKey, sourceRuns]);

  const keySelectOptions = useMemo(() => {
    if (!selectedChannel) return historyFilterOptions(sourceRuns).keys;
    return keyOptions.map(maskAPIKey);
  }, [selectedChannel, keyOptions, sourceRuns]);

  const effortOptions = useMemo(() => historyFilterOptions(sourceRuns).efforts, [sourceRuns]);

  const filter: HistoryFilter = useMemo(
    () => ({
      verdict,
      modelID,
      keyPrefix: apiKey ? maskAPIKey(apiKey) : undefined,
      // undefined means any level; the empty string is a real choice, so it has
      // to stay distinguishable from no filter.
      reasoningEffort: effort === undefined ? undefined : effort === NO_EFFORT ? '' : effort,
    }),
    [verdict, modelID, apiKey, effort]
  );

  const filteredRuns = useMemo(() => filterRuns(sourceRuns, filter), [sourceRuns, filter]);

  // Without a channel filter the page merges every channel's runs, keeping the
  // newest-first order and grouping runs by their channel.
  const groups = useMemo<ChannelRunGroup[]>(() => {
    const order: number[] = [];
    const byChannel = new Map<number, ChannelRunGroup>();

    for (const node of filteredRuns) {
      let group = byChannel.get(node.channelID);
      if (!group) {
        group = { channelID: node.channelID, channelName: node.channelName, edges: [] };
        byChannel.set(node.channelID, group);
        order.push(node.channelID);
      }
      group.edges.push({ node, cursor: node.id });
    }

    return order.map((id) => byChannel.get(id)!).filter(Boolean);
  }, [filteredRuns]);

  const loading = showingAll ? allRuns.isLoading : historyQuery.isLoading;
  const fetching = showingAll ? allRuns.isFetching : historyQuery.isFetching;
  const configured = (config?.targets.length ?? 0) > 0;
  const filterActive = isHistoryFilterActive(filter);

  const refresh = () => {
    if (showingAll) {
      void allRuns.refetch();
    } else {
      void historyQuery.refetch();
    }
  };

  const filters = (
    <div className='flex flex-wrap items-center gap-2'>
      {channelOptions.length > 0 && (
        <Select
          value={channelID ?? ALL_CHANNELS}
          onValueChange={(value) => setChannelID(value === ALL_CHANNELS ? undefined : value)}
        >
          <SelectTrigger className='w-44' aria-label={t('intelligence.history.channelPlaceholder')}>
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

      {/* A key belongs to exactly one channel, so this picker is only offered
          once the list is narrowed to a single channel. */}
      {!showingAll && keySelectOptions.length > 0 && (
        <Select
          value={apiKey ? maskAPIKey(apiKey) : ALL_KEYS}
          onValueChange={(value) => {
            if (value === ALL_KEYS) {
              setApiKey(undefined);
              return;
            }
            setApiKey(keyOptions.find((key) => maskAPIKey(key) === value));
          }}
        >
          <SelectTrigger className='w-44' aria-label={t('intelligence.history.keyPlaceholder')}>
            <SelectValue placeholder={t('intelligence.history.keyPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_KEYS}>{t('intelligence.history.allKeys')}</SelectItem>
            {keySelectOptions.map((masked) => (
              <SelectItem key={masked} value={masked}>
                {masked}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      {modelSelectOptions.length > 0 && (
        <Select
          value={modelID ?? ALL_MODELS}
          onValueChange={(value) => setModelID(value === ALL_MODELS ? undefined : value)}
        >
          <SelectTrigger className='w-44' aria-label={t('intelligence.history.modelPlaceholder')}>
            <SelectValue placeholder={t('intelligence.history.modelPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_MODELS}>{t('intelligence.history.allModels')}</SelectItem>
            {modelSelectOptions.map((model) => (
              <SelectItem key={model} value={model}>
                {model}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      {effortOptions.length > 0 && (
        <Select
          value={effort ?? ALL_EFFORTS}
          onValueChange={(value) => setEffort(value === ALL_EFFORTS ? undefined : value)}
        >
          <SelectTrigger className='w-40' aria-label={t('intelligence.history.effortPlaceholder')}>
            <SelectValue placeholder={t('intelligence.history.effortPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_EFFORTS}>{t('intelligence.history.allEfforts')}</SelectItem>
            <SelectItem value={NO_EFFORT}>{t('intelligence.history.effortDefault')}</SelectItem>
            {effortOptions.map((value) => (
              <SelectItem key={value} value={value}>
                {value}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      <Select value={verdict} onValueChange={(value) => setVerdict(value as VerdictFilter)}>
        <SelectTrigger className='w-44' aria-label={t('intelligence.history.verdictFilter.label')}>
          <SelectValue placeholder={t('intelligence.history.verdictFilter.label')} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value='all'>{t('intelligence.history.verdictFilter.all')}</SelectItem>
          <SelectItem value='degraded'>{t('intelligence.history.verdictFilter.degraded')}</SelectItem>
          <SelectItem value='inconclusive'>{t('intelligence.history.verdictFilter.inconclusive')}</SelectItem>
          <SelectItem value='failed'>{t('intelligence.history.verdictFilter.failed')}</SelectItem>
          <SelectItem value='normal'>{t('intelligence.history.verdictFilter.normal')}</SelectItem>
        </SelectContent>
      </Select>

      {filterActive && (
        <Button
          variant='ghost'
          size='sm'
          onClick={() => {
            setVerdict(DEFAULT_HISTORY_FILTER.verdict);
            setModelID(undefined);
            setApiKey(undefined);
            setEffort(undefined);
          }}
        >
          {t('intelligence.history.clearFilters')}
        </Button>
      )}
    </div>
  );

  const actions = (
    <div className='flex flex-wrap items-center gap-2'>
      {filters}
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
          {showingAll ? (
            groups.length > 0 ? (
              <div className='space-y-6'>
                {groups.map((group) => (
                  <div key={group.channelID} className='space-y-2'>
                    <div className='flex items-center gap-2'>
                      <h3 className='text-sm font-medium'>{group.channelName || '#' + group.channelID}</h3>
                      <span className='text-muted-foreground text-xs'>
                        {t('intelligence.history.groupCount', { count: group.edges.length })}
                      </span>
                    </div>
                    <IntelligenceHistoryList
                      history={toConnection(group.edges.map((edge) => edge.node))}
                      loading={false}
                      configured={configured}
                    />
                  </div>
                ))}
              </div>
            ) : (
              <IntelligenceHistoryList history={undefined} loading={loading} configured={configured} />
            )
          ) : (
            <IntelligenceHistoryList history={toConnection(filteredRuns)} loading={loading} configured={configured} />
          )}
        </TabsContent>

        <TabsContent value='settings'>
          <IntelligenceSettings config={config} loading={configLoading} readOnly={!canRun} />
        </TabsContent>
      </Tabs>
    </Main>
  );
}
