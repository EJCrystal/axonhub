'use client';

import { useEffect, useMemo, useState } from 'react';
import { IconGripVertical, IconPlayerPlay, IconPlus, IconTrash } from '@tabler/icons-react';
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core';
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useQueryChannels } from '@/features/channels/data/channels';
import { modelsForAPIKey } from '../data/api-key-models';
import { INTELLIGENCE_BENCHMARKS, REASONING_EFFORT_OPTIONS, benchmarkLabelKey } from '../data/benchmarks';
import { channelIdKey, sameChannelId } from '../data/channel-id';
import { useRunIntelligenceCheckNow, useSetIntelligenceConfig } from '../data/intelligence';
import { maskAPIKey } from '../data/mask-key';
import { INTELLIGENCE_INTERVALS, IntelligenceConfig, IntelligenceInterval } from '../data/schema';
import { IntelligenceAddTargetDialog } from './intelligence-add-target-dialog';

// Radix Select reserves the empty string, so "provider default" needs its own value.
const NO_EFFORT = '__default__';

// The per-run budget the backend applies when a target sets none, plus the cap
// it refuses to store. Kept in sync with orchestrator.intelligenceTotalTimeout
// and orchestrator.intelligenceMaxTimeoutMinutes.
const DEFAULT_TIMEOUT_MINUTES = 12;
const MAX_TIMEOUT_MINUTES = 20;

// The published benchmarks. The empty string means pelican, which is what every
// stored target used before the choice existed, so the picker shows the pelican
// default as its own explicit value.

interface Props {
  config?: IntelligenceConfig;
  loading: boolean;
  readOnly: boolean;
  // canRun gates the per-row run button, which needs the same write scope as
  // saving the configuration.
  canRun: boolean;
}

interface DraftTarget {
  channelID: string;
  apiKey: string;
  modelID: string;
  // Empty means "leave the thinking level alone".
  reasoningEffort: string;
  // Zero means "use the built-in per-run budget".
  timeoutMinutes: number;
  // Which check this row runs. Empty means the pelican default.
  benchmark: string;
}

export function IntelligenceSettings({ config, loading, readOnly, canRun }: Props) {
  const { t } = useTranslation();
  const save = useSetIntelligenceConfig();
  const runNow = useRunIntelligenceCheckNow();

  // The picker needs each channel's model list AND its credential set, and the
  // list query omits both unless the matching columns are visible. Ask for the
  // full node so the selectors have what they need.
  const { data: channels } = useQueryChannels({ first: 200, full: true });

  const [enabled, setEnabled] = useState(false);
  const [disableDegradedKeys, setDisableDegradedKeys] = useState(false);
  const [intervalMinutes, setIntervalMinutes] = useState<IntelligenceInterval>(60);
  const [targets, setTargets] = useState<DraftTarget[]>([]);
  // Which channel's key list is showing. Empty lets the first group claim it.
  const [activeGroupKey, setActiveGroupKey] = useState<string>('');

  useEffect(() => {
    if (!config) return;
    setEnabled(config.enabled);
    setDisableDegradedKeys(config.disableDegradedKeys ?? false);
    setIntervalMinutes((config.intervalMinutes as IntelligenceInterval) ?? 60);
    setTargets(
      config.targets.map((target) => ({
        channelID: target.channelID,
        apiKey: target.apiKey ?? '',
        modelID: target.modelID,
        reasoningEffort: target.reasoningEffort ?? '',
        timeoutMinutes: target.timeoutMinutes ?? 0,
        benchmark: target.benchmark ?? '',
      }))
    );
  }, [config]);

  const channelOptions = useMemo(
    () =>
      (channels?.edges ?? []).map((edge) => ({
        id: edge.node.id,
        name: edge.node.name,
        supportedModels: edge.node.supportedModels ?? [],
        credentials: edge.node.credentials ?? null,
        disabledAPIKeys: edge.node.disabledAPIKeys ?? [],
      })),
    [channels]
  );

  // A saved target stores its channel as a GID whose type segment used to be
  // written lowercase, so match on the numeric id rather than the raw string.
  const channelByID = (id: string) => channelOptions.find((channel) => sameChannelId(channel.id, id));

  // Enabled keys only: a disabled key cannot be exercised by the check.
  const keysFor = (channelID: string) => {
    const channel = channelByID(channelID);
    if (!channel) return [];
    const disabled = new Set((channel.disabledAPIKeys ?? []).map((item) => item.key));
    const all = [...(channel.credentials?.apiKeys ?? [])];
    const single = channel.credentials?.apiKey;
    if (single && !all.includes(single)) all.unshift(single);
    return all.filter((key) => key.trim() !== '' && !disabled.has(key));
  };

  const modelsFor = (channelID: string, apiKey: string) => {
    const channel = channelByID(channelID);
    if (!channel) return [];
    return modelsForAPIKey(channel.credentials, channel.supportedModels, apiKey);
  };

  // A channel may be added several times, once per key: the key is what the row
  // is about, and each one runs its own model and thinking level. What must not
  // repeat is the channel+key pair.
  const isPairConfigured = (channelID: string, apiKey: string, skipIndex?: number) =>
    targets.some((target, index) => index !== skipIndex && sameChannelId(target.channelID, channelID) && target.apiKey === apiKey);

  // The first pair still free. Returns null when every enabled key of every
  // channel is already configured, so the caller can keep the button disabled.
  const firstFreePair = (): { channelID: string; apiKey: string } | null => {
    for (const channel of channelOptions) {
      for (const apiKey of keysFor(channel.id)) {
        if (!isPairConfigured(channel.id, apiKey)) {
          return { channelID: channel.id, apiKey };
        }
      }
    }

    return null;
  };

  // Keys still free on a channel, so its row only offers what is not taken.
  const freeKeysFor = (channelID: string, skipIndex: number) =>
    keysFor(channelID).filter((key) => !isPairConfigured(channelID, key, skipIndex));

  // Adding a target asks for its channel, key, model and check first: inserting
  // a row on the first free pair left the reader to correct every field by hand.
  const [addOpen, setAddOpen] = useState(false);

  const dialogChannels = useMemo(
    () =>
      channelOptions.map((channel) => ({
        id: channel.id,
        name: channel.name,
        keys: keysFor(channel.id),
        modelsFor: (apiKey: string) => modelsFor(channel.id, apiKey),
      })),
    // keysFor and modelsFor read the same channel options, so the list is rebuilt
    // whenever they change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [channelOptions]
  );

  const addTarget = (target: DraftTarget) => {
    setTargets((prev) => [...prev, target]);
  };

  const updateTarget = (index: number, patch: Partial<DraftTarget>) => {
    setTargets((prev) => prev.map((target, i) => (i === index ? { ...target, ...patch } : target)));
  };

  const handleSave = () => {
    save.mutate({
      enabled,
      intervalMinutes,
      disableDegradedKeys,
      targets: targets.map((target) => ({
        channelID: target.channelID,
        modelID: target.modelID,
        apiKey: target.apiKey,
        // Omit rather than send an empty string, so the backend stores nothing.
        reasoningEffort: target.reasoningEffort || undefined,
        // Zero means the built-in budget, so omit it instead of storing a value.
        timeoutMinutes: target.timeoutMinutes || undefined,
        // Empty means the pelican default, so omit it rather than storing one.
        benchmark: target.benchmark || undefined,
      })),
    });
  };

  // Grouping is by channel, so the reader sees one block per channel instead of
  // a flat list where three keys of the same channel sit rows apart. The groups
  // keep the order the targets were saved in, and each group keeps its own order
  // inside, which is the order the drag handle edits.
  const groups = useMemo(() => {
    const order: string[] = [];
    const byChannel = new Map<string, { channelID: string; indices: number[] }>();

    targets.forEach((target, index) => {
      const key = channelIdKey(target.channelID);
      const existing = byChannel.get(key);
      if (existing) {
        existing.indices.push(index);
        return;
      }
      order.push(key);
      byChannel.set(key, { channelID: target.channelID, indices: [index] });
    });

    return order.map((key) => byChannel.get(key)!);
  }, [targets]);

  // The channel a card currently holds, read back from the targets so switching
  // turns the whole card over at once rather than leaving its keys behind.
  const channelOfGroup = (group: { indices: number[] }) => targets[group.indices[0]]?.channelID ?? '';

  // A saved target may name a channel the list no longer carries, and the card
  // title still has to say something. The stored id is the last resort.
  const channelNameOf = (channelID: string) => channelByID(channelID)?.name ?? channelID;

  // Reordering happens inside one channel, so a drag can never land a key on a
  // channel it does not belong to. The move is applied to the underlying index
  // list, which leaves every other channel's rows where they were.
  const reorderWithinGroup = (group: { indices: number[] }, from: number, to: number) => {
    const fromIndex = group.indices[from];
    const toIndex = group.indices[to];
    if (fromIndex === undefined || toIndex === undefined || fromIndex === toIndex) return;

    setTargets((prev) => arrayMove(prev, fromIndex, toIndex));
  };

  // Removing a card takes its rows with it; removing one row leaves the card.
  const removeGroup = (group: { indices: number[] }) => {
    setTargets((prev) => prev.filter((_, index) => !group.indices.includes(index)));
  };

  const removeTargetRow = (index: number) => {
    setTargets((prev) => prev.filter((_, i) => i !== index));
  };

  // A row may be run once the backend has stored exactly this channel, key and
  // model. A newly added or edited row cannot be run yet, because the run reads
  // the saved configuration.
  const isRowSaved = (index: number) => {
    const draft = targets[index];
    if (!draft) return false;

    return (config?.targets ?? []).some(
      (saved) => sameChannelId(saved.channelID, draft.channelID) && (saved.apiKey ?? '') === draft.apiKey && saved.modelID === draft.modelID
    );
  };

  // The tab selection has to follow the groups: removing the channel that was
  // showing would otherwise leave the panel empty, with the reader's rows in a
  // tab no longer rendered.
  useEffect(() => {
    const keys = groups.map((group) => channelIdKey(channelOfGroup(group)));
    if (keys.length === 0) {
      if (activeGroupKey !== '') setActiveGroupKey('');
      return;
    }
    if (!keys.includes(activeGroupKey)) setActiveGroupKey(keys[0]);
    // channelOfGroup reads the live targets, so the group list is the dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groups, activeGroupKey]);

  // Two cards, because the screen holds two different jobs: deciding when the
  // check runs, and deciding what it runs against. One card for both buried the
  // schedule under a list that can be long.
  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('intelligence.settings.scheduleTitle')}</CardTitle>
          <CardDescription>{t('intelligence.settings.scheduleDescription')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4'>
            <div className='space-y-1'>
              <Label htmlFor='intelligence-enabled'>{t('intelligence.settings.enabled')}</Label>
              <p className='text-muted-foreground text-xs'>{t('intelligence.settings.enabledHint')}</p>
            </div>
            <Switch id='intelligence-enabled' checked={enabled} onCheckedChange={setEnabled} disabled={readOnly} />
          </div>

          <div className='flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4'>
            <div className='space-y-1'>
              <Label>{t('intelligence.settings.interval')}</Label>
              <p className='text-muted-foreground text-xs'>{t('intelligence.settings.intervalHint')}</p>
            </div>
            <Select
              value={String(intervalMinutes)}
              onValueChange={(value) => setIntervalMinutes(Number(value) as IntelligenceInterval)}
              disabled={readOnly}
            >
              <SelectTrigger className='w-40' data-testid='intelligence-interval'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INTELLIGENCE_INTERVALS.map((minutes) => (
                  <SelectItem key={minutes} value={String(minutes)}>
                    {t('intelligence.interval.' + minutes)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className='flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4'>
            <div className='space-y-1'>
              <Label htmlFor='intelligence-disable-degraded'>{t('intelligence.settings.disableDegradedKeys')}</Label>
              <p className='text-muted-foreground max-w-prose text-xs'>{t('intelligence.settings.disableDegradedKeysHint')}</p>
            </div>
            <Switch
              id='intelligence-disable-degraded'
              checked={disableDegradedKeys}
              onCheckedChange={setDisableDegradedKeys}
              disabled={readOnly}
              data-testid='intelligence-disable-degraded'
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('intelligence.settings.targetsTitle')}</CardTitle>
          <CardDescription>{t('intelligence.settings.description')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='flex items-center justify-between'>
            <Label className='flex items-center gap-2'>
              {t('intelligence.settings.targets')}
              <span className='text-muted-foreground text-xs tabular-nums'>({targets.length})</span>
            </Label>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setAddOpen(true)}
              disabled={readOnly || loading || !firstFreePair()}
              data-testid='add-intelligence-target'
            >
              <IconPlus className='mr-1 h-4 w-4' />
              {t('intelligence.settings.addTarget')}
            </Button>
          </div>

          {targets.length === 0 ? (
            <p className='text-muted-foreground rounded-lg border border-dashed py-10 text-center text-xs'>
              {t('intelligence.settings.noTargets')}
            </p>
          ) : (
            // The channels run across the top, the way the history view lists
            // them: one channel's keys are read at a time, and a channel with a
            // long key list no longer buries the next one several screens down.
            <Tabs
              // The tabs own their selection, falling back to the first group
              // so a channel removed elsewhere cannot leave the panel empty.
              value={activeGroupKey}
              onValueChange={setActiveGroupKey}
              className='space-y-3'
            >
              <TabsList className='h-auto w-full flex-wrap justify-start gap-1'>
                {groups.map((group) => {
                  const key = channelIdKey(channelOfGroup(group));
                  return (
                    <TabsTrigger key={key} value={key} className='flex-none px-3' data-testid='intelligence-channel-tab'>
                      {channelNameOf(channelOfGroup(group))}
                      <span className='text-muted-foreground ml-1 text-xs'>({group.indices.length})</span>
                    </TabsTrigger>
                  );
                })}
              </TabsList>
              {groups.map((group) => {
                const key = channelIdKey(channelOfGroup(group));
                return (
                  <TabsContent key={key} value={key}>
                    <ChannelTargetCard
                      group={group}
                      targets={targets}
                      channelName={channelNameOf(channelOfGroup(group))}
                      readOnly={readOnly}
                      canRun={canRun}
                      runPending={runNow.isPending}
                      isSavedRow={isRowSaved}
                      modelsFor={modelsFor}
                      freeKeysFor={freeKeysFor}
                      onUpdate={updateTarget}
                      onRemoveRow={removeTargetRow}
                      onRemoveGroup={() => removeGroup(group)}
                      onReorder={(from, to) => reorderWithinGroup(group, from, to)}
                      onRun={(target) => runNow.mutate({ channelID: target.channelID, apiKey: target.apiKey })}
                    />
                  </TabsContent>
                );
              })}
            </Tabs>
          )}

          <p className='text-muted-foreground text-xs'>{t('intelligence.settings.targetsHint')}</p>
          <p className='text-muted-foreground text-xs'>{t('intelligence.settings.dragHint')}</p>

          <div className='flex justify-end'>
            <Button onClick={handleSave} disabled={readOnly || save.isPending} data-testid='save-intelligence-config'>
              {save.isPending ? t('intelligence.settings.saving') : t('intelligence.settings.save')}
            </Button>
          </div>

          <IntelligenceAddTargetDialog
            open={addOpen}
            onOpenChange={setAddOpen}
            channels={dialogChannels}
            isPairConfigured={(channelID, apiKey) =>
              targets.some((target) => sameChannelId(target.channelID, channelID) && target.apiKey === apiKey)
            }
            onAdd={(target) => addTarget({ ...target, timeoutMinutes: 0 })}
          />
        </CardContent>
      </Card>
    </div>
  );
}

interface TargetGroup {
  channelID: string;
  indices: number[];
}

interface ChannelCardProps {
  group: TargetGroup;
  targets: DraftTarget[];
  channelName: string;
  readOnly: boolean;
  canRun: boolean;
  runPending: boolean;
  isSavedRow: (index: number) => boolean;
  modelsFor: (channelID: string, apiKey: string) => string[];
  freeKeysFor: (channelID: string, skipIndex: number) => string[];
  onUpdate: (index: number, patch: Partial<DraftTarget>) => void;
  onRemoveRow: (index: number) => void;
  onRemoveGroup: () => void;
  onReorder: (from: number, to: number) => void;
  onRun: (target: DraftTarget) => void;
}

// ChannelTargetCard holds one channel's keys.
//
// The channel is what the card is: it is chosen by the tab above, so the card
// carries no channel picker of its own. That used to be a trap — switching a
// card with three keys to a channel that has one left rows pointing at keys the
// channel does not have — and the picker is gone rather than patched.
//
// What the card does own is the order of its rows: the reader drags them, and
// that order is what gets saved.
function ChannelTargetCard({
  group,
  targets,
  channelName,
  readOnly,
  canRun,
  runPending,
  isSavedRow,
  modelsFor,
  freeKeysFor,
  onUpdate,
  onRemoveRow,
  onRemoveGroup,
  onReorder,
  onRun,
}: ChannelCardProps) {
  const { t } = useTranslation();

  // Drag ends are reported by row key, so the ids must be stable across renders:
  // they are the target's position in the whole list, which is what the move is
  // applied to.
  const itemIDs = group.indices.map((index) => String(index));
  const [activeID, setActiveID] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    setActiveID(null);
    if (!over || active.id === over.id) return;

    const from = group.indices.findIndex((index) => String(index) === String(active.id));
    const to = group.indices.findIndex((index) => String(index) === String(over.id));
    if (from < 0 || to < 0) return;

    onReorder(from, to);
  };

  return (
    <div className='rounded-lg border' data-testid='intelligence-channel-card'>
      <div className='bg-muted/40 flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2'>
        <div className='flex items-center gap-2'>
          <span className='text-sm font-medium'>{channelName}</span>
          <Badge variant='outline' className='h-5 px-1.5 text-[11px]'>
            {t('intelligence.settings.keyCount', { count: group.indices.length })}
          </Badge>
        </div>
        <Button variant='ghost' size='icon' className='h-7 w-7' onClick={onRemoveGroup} disabled={readOnly} aria-label={t('common.delete')}>
          <IconTrash size={15} />
        </Button>
      </div>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={({ active }) => setActiveID(String(active.id))}
        onDragCancel={() => setActiveID(null)}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={itemIDs} strategy={verticalListSortingStrategy}>
          <div className='divide-y'>
            {group.indices.map((index, position) => {
              const target = targets[index];
              if (!target) return null;

              return (
                <SortableTargetRow
                  key={String(index)}
                  id={String(index)}
                  position={position}
                  target={target}
                  index={index}
                  dragging={activeID === String(index)}
                  readOnly={readOnly}
                  canRun={canRun}
                  runPending={runPending}
                  saved={isSavedRow(index)}
                  keys={(() => {
                    const keys = freeKeysFor(target.channelID, index);
                    // Keep the row's own key selectable even when it is the
                    // reason the pair reads as taken, so the current value still
                    // renders.
                    if (target.apiKey && !keys.includes(target.apiKey)) keys.unshift(target.apiKey);
                    return keys;
                  })()}
                  models={modelsFor(target.channelID, target.apiKey)}
                  modelsFor={modelsFor}
                  onUpdate={onUpdate}
                  onRemove={onRemoveRow}
                  onRun={onRun}
                />
              );
            })}
          </div>
        </SortableContext>
      </DndContext>
    </div>
  );
}

interface SortableRowProps {
  id: string;
  position: number;
  index: number;
  target: DraftTarget;
  dragging: boolean;
  readOnly: boolean;
  canRun: boolean;
  runPending: boolean;
  saved: boolean;
  keys: string[];
  models: string[];
  modelsFor: (channelID: string, apiKey: string) => string[];
  onUpdate: (index: number, patch: Partial<DraftTarget>) => void;
  onRemove: (index: number) => void;
  onRun: (target: DraftTarget) => void;
}

// SortableTargetRow is one key of a channel: which key, and what the check asks
// of it. The handle is the only drag start, so the selects and the input inside
// the row keep working normally.
function SortableTargetRow({
  id,
  position,
  index,
  target,
  dragging,
  readOnly,
  canRun,
  runPending,
  saved,
  keys,
  models,
  modelsFor,
  onUpdate,
  onRemove,
  onRun,
}: SortableRowProps) {
  const { t } = useTranslation();
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition } = useSortable({
    id,
    disabled: readOnly,
  });

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      data-testid='intelligence-target-row'
      className={`flex flex-wrap items-center gap-2 px-3 py-2 ${dragging ? 'bg-accent/40' : ''}`}
    >
      <button
        type='button'
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        className='text-muted-foreground hover:text-foreground cursor-grab active:cursor-grabbing disabled:cursor-not-allowed'
        disabled={readOnly}
        aria-label={t('intelligence.settings.dragKey', { key: target.apiKey ? maskAPIKey(target.apiKey) : position + 1 })}
        data-testid='intelligence-target-drag-handle'
      >
        <IconGripVertical size={16} />
      </button>

      <Select
        value={target.apiKey}
        onValueChange={(value) => onUpdate(index, { apiKey: value, modelID: modelsFor(target.channelID, value)[0] ?? '' })}
        disabled={readOnly || keys.length === 0}
      >
        <SelectTrigger className='h-8 w-40' data-testid='intelligence-target-key'>
          <SelectValue placeholder={t('intelligence.settings.keyPlaceholder')} />
        </SelectTrigger>
        <SelectContent>
          {keys.map((key) => (
            <SelectItem key={key} value={key}>
              <span className='font-mono text-xs'>{maskAPIKey(key)}</span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select
        value={target.modelID}
        onValueChange={(value) => onUpdate(index, { modelID: value })}
        disabled={readOnly || models.length === 0}
      >
        <SelectTrigger className='h-8 w-44'>
          <SelectValue placeholder={t('intelligence.settings.modelPlaceholder')} />
        </SelectTrigger>
        <SelectContent>
          {models.map((model) => (
            <SelectItem key={model} value={model}>
              {model}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Not every model accepts every level, and an empty choice lets the
          provider apply its own default. */}
      <Select
        value={target.reasoningEffort || NO_EFFORT}
        onValueChange={(value) => onUpdate(index, { reasoningEffort: value === NO_EFFORT ? '' : value })}
        disabled={readOnly}
      >
        <SelectTrigger className='h-8 w-36'>
          <SelectValue placeholder={t('intelligence.settings.effortPlaceholder')} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NO_EFFORT}>{t('intelligence.settings.effortDefault')}</SelectItem>
          {REASONING_EFFORT_OPTIONS.map((effort) => (
            <SelectItem key={effort} value={effort}>
              {effort}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Zero keeps the built-in budget. A slow upstream can ask for more time
          without changing anything else. */}
      <Input
        type='number'
        min={0}
        max={MAX_TIMEOUT_MINUTES}
        value={target.timeoutMinutes || ''}
        placeholder={String(DEFAULT_TIMEOUT_MINUTES)}
        onChange={(event) => {
          const parsed = Number(event.target.value);
          onUpdate(index, {
            timeoutMinutes: Number.isFinite(parsed) && parsed > 0 ? Math.min(parsed, MAX_TIMEOUT_MINUTES) : 0,
          });
        }}
        disabled={readOnly}
        className='h-8 w-20'
      />

      <Select
        value={target.benchmark || INTELLIGENCE_BENCHMARKS[0]}
        onValueChange={(value) => onUpdate(index, { benchmark: value === INTELLIGENCE_BENCHMARKS[0] ? '' : value })}
        disabled={readOnly}
      >
        <SelectTrigger className='h-8 w-28'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {INTELLIGENCE_BENCHMARKS.map((value) => (
            <SelectItem key={value} value={value}>
              {t(benchmarkLabelKey(value))}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <div className='ml-auto flex items-center gap-1'>
        {/* Runs the saved target, not the row as edited: the backend works from
            the stored configuration, so an unsaved change would be ignored. */}
        <Button
          variant='outline'
          size='sm'
          className='h-7'
          disabled={!canRun || readOnly || runPending || !saved}
          onClick={() => onRun(target)}
          aria-label={t('intelligence.settings.runThisTarget')}
          data-testid='run-intelligence-target'
        >
          <IconPlayerPlay size={14} className='mr-1' />
          {t('intelligence.settings.runThisTarget')}
        </Button>
        <Button variant='ghost' size='icon' className='h-7 w-7' onClick={() => onRemove(index)} disabled={readOnly}>
          <IconTrash size={15} />
        </Button>
      </div>
    </div>
  );
}
