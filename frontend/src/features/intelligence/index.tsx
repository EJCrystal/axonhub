'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconBrain, IconChevronDown, IconChevronUp, IconFilter, IconRefresh } from '@tabler/icons-react';
import { Main } from '@/components/layout/main';
import { PageHeader } from '@/components/layout/page-header';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { usePermissions } from '@/hooks/usePermissions';
import { useQueryChannels } from '@/features/channels/data/channels';
import { IntelligenceSettings } from './components/intelligence-settings';
import { IntelligenceHistoryList } from './components/intelligence-history-list';
import { channelIdKey } from './data/channel-id';
import { maskAPIKey } from './data/mask-key';
import {
  DEFAULT_HISTORY_FILTER,
  HistoryFilter,
  VerdictFilter,
  filterRuns,
  groupRunsByChannel,
  historyFilterOptions,
  isHistoryFilterActive,
  pickActiveChannel,
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
const ALL_BENCHMARKS = '__all_benchmarks__';
// The stored benchmark is empty for the pelican default, but Radix Select
// reserves the empty string, so the option needs a value of its own.
const PELICAN_OPTION = '__pelican__';

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
  const [benchmark, setBenchmark] = useState<string>();
  // Which channel's records the merged view shows. Empty means "not chosen yet",
  // so the first channel with records can claim the tab on its own.
  const [activeChannelTab, setActiveChannelTab] = useState<string>('');
  // Which top-level tab is showing, so the header actions can follow it.
  const [activeTab, setActiveTab] = useState<string>('history');

  // The history view follows the test configuration: only the channels that are
  // currently configured get a filter entry or a channel tab, so removing a
  // channel there also removes it from the history. The full channel query stays
  // because the key picker needs each channel's credentials.
  const { data: channels } = useQueryChannels({ first: 200, full: true });
  const channelByID = useMemo(() => {
    const byID = new Map<string, { id: string; name: string }>();
    for (const edge of channels?.edges ?? []) {
      byID.set(channelIdKey(edge.node.id), { id: edge.node.id, name: edge.node.name });
    }
    return byID;
  }, [channels]);

  const configuredChannelIDs = useMemo(() => {
    const ids: string[] = [];
    for (const target of config?.targets ?? []) {
      const id = channelIdKey(target.channelID);
      if (!ids.includes(id)) ids.push(id);
    }
    return ids;
  }, [config]);

  const channelOptions = useMemo(
    () =>
      configuredChannelIDs.map((id) => {
        const known = channelByID.get(id);
        return {
          id: known?.id ?? id,
          name: known?.name ?? t('intelligence.history.channelFallback', { id }),
        };
      }),
    [configuredChannelIDs, channelByID, t]
  );

  const allRuns = useAllIntelligenceRuns(200);
  const historyQuery = useIntelligenceHistory(channelID);
  const showingAll = !channelID;

  // Switching channel invalidates the key and model narrowing, since both were
  // chosen against the previous channel's credentials.
  useEffect(() => {
    setApiKey(undefined);
    setBenchmark(undefined);
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

  const keySelectOptions = useMemo(() => {
    if (!selectedChannel) return historyFilterOptions(sourceRuns).keys;
    return keyOptions.map(maskAPIKey);
  }, [selectedChannel, keyOptions, sourceRuns]);

  const filter: HistoryFilter = useMemo(
    () => ({
      verdict,
      keyPrefix: apiKey ? maskAPIKey(apiKey) : undefined,
      // The empty value is the pelican default, so it is a choice of its own.
      benchmark: benchmark === undefined ? undefined : benchmark,
    }),
    [verdict, apiKey, benchmark]
  );

  const filteredRuns = useMemo(() => filterRuns(sourceRuns, filter), [sourceRuns, filter]);

  // Without a channel filter the page merges every channel's runs, keeping the
  // newest-first order and grouping runs by their channel.
  const groups = useMemo<ChannelRunGroup[]>(() => {
    return (
      groupRunsByChannel(filteredRuns)
        // History follows the configuration, so a channel removed from the
        // settings tab stops appearing here even though its rows still exist.
        .filter((group) => configuredChannelIDs.includes(String(group.channelID)))
        .map((group) => ({
          channelID: group.channelID,
          channelName: group.channelName,
          edges: group.runs.map((node) => ({ node, cursor: node.id })),
        }))
    );
  }, [filteredRuns, configuredChannelIDs]);

  // The channel the merged view is showing. The filter can remove the channel
  // that was selected, so fall back to the first one that still has records
  // rather than rendering nothing.
  const selectedGroup = useMemo(() => pickActiveChannel(groups, activeChannelTab), [groups, activeChannelTab]);

  const loading = showingAll ? allRuns.isLoading : historyQuery.isLoading;
  const fetching = showingAll ? allRuns.isFetching : historyQuery.isFetching;
  const configured = (config?.targets.length ?? 0) > 0;
  const filterActive = isHistoryFilterActive(filter);
  // The count shown on the disclosure: how many axes are narrowing the list.
  const filterCount = [benchmark !== undefined, apiKey !== undefined, verdict !== 'all'].filter(Boolean).length;

  const refresh = () => {
    if (showingAll) {
      void allRuns.refetch();
    } else {
      void historyQuery.refetch();
    }
  };

  // The three filters are one job, so they fold into a disclosure. It starts
  // open when something already narrows the list, and closed otherwise, so the
  // header stays a single line of controls.
  const [filtersOpen, setFiltersOpen] = useState(filterActive);

  const filters = (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          variant={filterActive ? 'secondary' : 'outline'}
          size='sm'
          onClick={() => setFiltersOpen((open) => !open)}
          aria-expanded={filtersOpen}
          data-testid='toggle-history-filters'
        >
          <IconFilter className='mr-1 h-4 w-4' />
          {t('intelligence.history.filters')}
          {filterCount > 0 && (
            <span className='bg-primary text-primary-foreground ml-1 rounded-full px-1.5 text-[10px] tabular-nums'>
              {filterCount}
            </span>
          )}
          {filtersOpen ? <IconChevronUp size={14} className='ml-1' /> : <IconChevronDown size={14} className='ml-1' />}
        </Button>
      </div>

      {filtersOpen && (
        <div className='flex flex-wrap items-center gap-2 rounded-lg border p-2'>
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

          {/* Both benchmarks are always offered: a reader may want to ask for one
            the current history has no rows for yet. */}
          <Select
            value={benchmark === undefined ? ALL_BENCHMARKS : benchmark || PELICAN_OPTION}
            onValueChange={(value) => {
              if (value === ALL_BENCHMARKS) return setBenchmark(undefined);
              return setBenchmark(value === PELICAN_OPTION ? '' : value);
            }}
          >
            <SelectTrigger className='w-36' aria-label={t('intelligence.history.benchmarkFilter')}>
              <SelectValue placeholder={t('intelligence.history.benchmarkFilter')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_BENCHMARKS}>{t('intelligence.history.allBenchmarks')}</SelectItem>
              <SelectItem value={PELICAN_OPTION}>{t('intelligence.settings.benchmarkPelican')}</SelectItem>
              <SelectItem value='candy'>{t('intelligence.settings.benchmarkCandy')}</SelectItem>
            </SelectContent>
          </Select>

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
              setApiKey(undefined);
              setBenchmark(undefined);
            }}
          >
            {t('intelligence.history.clearFilters')}
          </Button>
        )}
        </div>
      )}
    </div>
  );


  // One menu entry per configured target. A channel can appear several times, so
  // the entry has to say which key it will exercise and with what — a bare
  // channel name would leave two rows looking identical.
  const configuredTargets = useMemo(
    () =>
      (config?.targets ?? []).map((target) => ({
        channelID: target.channelID,
        channelName:
          target.channelName ||
          channelOptions.find((option) => option.id === target.channelID)?.name ||
          '',
        modelID: target.modelID,
        apiKey: target.apiKey ?? '',
        keyLabel: target.apiKey ? maskAPIKey(target.apiKey) : '',
        reasoningEffort: target.reasoningEffort ?? '',
      })),
    [config, channelOptions]
  );

  const runDisabled =
    !canRun ||
    runNow.isPending ||
    (config?.targets.length ?? 0) === 0 ||
    configuredTargets.length === 0;

  // The run button starts every configured target, so it is labelled with the
  // count: "immediately test" alone reads as if it covered the row in view.
  const runTargetLabel = t('intelligence.runAllTargets', { count: configuredTargets.length });

  const runButton = (
    <Button
      size='sm'
      onClick={() => runNow.mutate(undefined)}
      disabled={runDisabled || runNow.isPending}
      data-testid='run-intelligence-now'
      title={t('intelligence.runAllTargetsHint')}
    >
      <IconBrain className='mr-1 h-4 w-4' />
      {runNow.isPending ? t('intelligence.running') : runTargetLabel}
    </Button>
  );

  // Each tab owns its own header actions: the history filters mean nothing on
  // the settings screen, and the run button is not a configuration step.
  const historyActions = (
    <div className='flex flex-wrap items-center gap-2'>
      {filters}
      <Button variant='outline' size='sm' onClick={refresh} disabled={fetching}>
        <IconRefresh className='mr-1 h-4 w-4' />
        {t('intelligence.history.refresh')}
      </Button>
      {runButton}
    </div>
  );

  // The settings screen adds, edits and runs targets row by row, so it gets no
  // header actions at all: a "test everything" button there would sit next to
  // "add channel" and read as another editing step.
  const settingsActions = undefined;

  return (
    <Main>
      <PageHeader
        title={t('intelligence.title')}
        description={t('intelligence.description')}
        // The actions describe the visible tab, and the run button says how many
        // targets it will start.
        actions={activeTab === 'settings' ? settingsActions : historyActions}
      />

      <Tabs value={activeTab} onValueChange={setActiveTab} className='space-y-4'>
        <TabsList>
          <TabsTrigger value='history'>{t('intelligence.tabs.history')}</TabsTrigger>
          <TabsTrigger value='settings'>{t('intelligence.tabs.settings')}</TabsTrigger>
        </TabsList>

        <TabsContent value='history'>
          {showingAll ? (
            groups.length > 0 && selectedGroup ? (
              // Channels run across the top rather than down the page: with a
              // long history per channel, a vertical stack buries the one the
              // reader wants below several screens of records.
              <Tabs
                // The inner tabs own their selection: a controlled value that
                // falls back to the first group before the reader picks one
                // leaves Radix with a value that names no rendered panel.
                defaultValue={String(selectedGroup.channelID)}
                onValueChange={setActiveChannelTab}
                className='space-y-3'
              >
                <TabsList className='h-auto w-full flex-wrap justify-start gap-1'>
                  {groups.map((group) => (
                    <TabsTrigger key={group.channelID} value={String(group.channelID)} className='flex-none px-3'>
                      {group.channelName || '#' + group.channelID}
                      <span className='text-muted-foreground ml-1 text-xs'>({group.edges.length})</span>
                    </TabsTrigger>
                  ))}
                </TabsList>
                <TabsContent value={String(selectedGroup.channelID)}>
                  <IntelligenceHistoryList
                    history={toConnection(selectedGroup.edges.map((edge) => edge.node))}
                    loading={false}
                    configured={configured}
                  />
                </TabsContent>
              </Tabs>
            ) : (
              <IntelligenceHistoryList history={undefined} loading={loading} configured={configured} />
            )
          ) : (
            <IntelligenceHistoryList history={toConnection(filteredRuns)} loading={loading} configured={configured} />
          )}
        </TabsContent>

        <TabsContent value='settings'>
          <IntelligenceSettings config={config} loading={configLoading} readOnly={!canRun} canRun={canRun} />
        </TabsContent>
      </Tabs>
    </Main>
  );
}
