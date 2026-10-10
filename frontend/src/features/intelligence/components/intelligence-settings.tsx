'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconPlayerPlay, IconPlus, IconTrash } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { useQueryChannels } from '@/features/channels/data/channels';
import { modelsForAPIKey } from '../data/api-key-models';
import { maskAPIKey } from '../data/mask-key';
import { sameChannelId } from '../data/channel-id';
import { INTELLIGENCE_INTERVALS, IntelligenceConfig, IntelligenceInterval } from '../data/schema';
import { INTELLIGENCE_BENCHMARKS, REASONING_EFFORT_OPTIONS, benchmarkLabelKey } from '../data/benchmarks';
import { IntelligenceAddTargetDialog } from './intelligence-add-target-dialog';
import { useRunIntelligenceCheckNow, useSetIntelligenceConfig } from '../data/intelligence';

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
  const [intervalMinutes, setIntervalMinutes] = useState<IntelligenceInterval>(60);
  const [targets, setTargets] = useState<DraftTarget[]>([]);

  useEffect(() => {
    if (!config) return;
    setEnabled(config.enabled);
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
    targets.some(
      (target, index) =>
        index !== skipIndex && sameChannelId(target.channelID, channelID) && target.apiKey === apiKey
    );

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

  const removeTarget = (index: number) => {
    setTargets((prev) => prev.filter((_, i) => i !== index));
  };

  // isSavedTarget reports whether this row still matches something the backend
  // has stored. A newly added or edited row cannot be run yet, because the run
  // reads the saved configuration.
  const isSavedTarget = (index: number) => {
    const draft = targets[index];
    if (!draft) return false;

    return (config?.targets ?? []).some(
      (saved) =>
        sameChannelId(saved.channelID, draft.channelID) &&
        (saved.apiKey ?? '') === draft.apiKey &&
        saved.modelID === draft.modelID
    );
  };

  const handleSave = () => {
    save.mutate({
      enabled,
      intervalMinutes,
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

  // Two cards, because the screen holds two different jobs: deciding when the
  // check runs, and deciding what it runs against. One card for both buried the
  // schedule under a table that can be long.
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
                    {t(`intelligence.interval.${minutes}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('intelligence.settings.targetsTitle')}</CardTitle>
          <CardDescription>{t('intelligence.settings.description')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
        <div className='space-y-2'>
          <div className='flex items-center justify-between'>
            <Label className='flex items-center gap-2'>
              {t('intelligence.settings.targets')}
              <span className='text-muted-foreground text-xs tabular-nums'>({targets.length})</span>
            </Label>
            <Button variant='outline' size='sm' onClick={() => setAddOpen(true)} disabled={readOnly || loading || !firstFreePair()} data-testid='add-intelligence-target'>
              <IconPlus className='mr-1 h-4 w-4' />
              {t('intelligence.settings.addTarget')}
            </Button>
          </div>

          <div className='rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('intelligence.settings.channelColumn')}</TableHead>
                  <TableHead>{t('intelligence.settings.keyColumn')}</TableHead>
                  <TableHead>{t('intelligence.settings.modelColumn')}</TableHead>
                  <TableHead>{t('intelligence.settings.effortColumn')}</TableHead>
                  <TableHead>{t('intelligence.settings.timeoutColumn')}</TableHead>
                  <TableHead>{t('intelligence.settings.benchmarkColumn')}</TableHead>
                  <TableHead className='w-16'></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {targets.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className='text-muted-foreground text-center text-xs'>
                      {t('intelligence.settings.noTargets')}
                    </TableCell>
                  </TableRow>
                )}
                {targets.map((target, index) => {
                  const keys = freeKeysFor(target.channelID, index);
                  // Keep the row's own key selectable even when it is the reason
                  // the pair reads as taken, so the current value still renders.
                  if (target.apiKey && !keys.includes(target.apiKey)) keys.unshift(target.apiKey);
                  const models = modelsFor(target.channelID, target.apiKey);
                  return (
                    <TableRow key={`${target.channelID}-${index}`}>
                      <TableCell>
                        <Select
                          value={target.channelID}
                          onValueChange={(value) => {
                            const apiKey = keysFor(value).find((key) => !isPairConfigured(value, key, index)) ?? '';
                            updateTarget(index, { channelID: value, apiKey, modelID: modelsFor(value, apiKey)[0] ?? '' });
                          }}
                          disabled={readOnly}
                        >
                          <SelectTrigger className='w-44'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {channelOptions.map((channel) => (
                                <SelectItem key={channel.id} value={channel.id}>
                                  {channel.name}
                                </SelectItem>
                              ))}
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell>
                        <Select
                          value={target.apiKey}
                          onValueChange={(value) => updateTarget(index, { apiKey: value, modelID: modelsFor(target.channelID, value)[0] ?? '' })}
                          disabled={readOnly || keys.length === 0}
                        >
                          <SelectTrigger className='w-40'>
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
                      </TableCell>
                      <TableCell>
                        <Select
                          value={target.modelID}
                          onValueChange={(value) => updateTarget(index, { modelID: value })}
                          disabled={readOnly || models.length === 0}
                        >
                          <SelectTrigger className='w-44'>
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
                      </TableCell>
                      <TableCell>
                        {/* Not every model accepts every level, and an empty
                            choice lets the provider apply its own default. */}
                        <Select
                          value={target.reasoningEffort || NO_EFFORT}
                          onValueChange={(value) =>
                            updateTarget(index, { reasoningEffort: value === NO_EFFORT ? '' : value })
                          }
                          disabled={readOnly}
                        >
                          <SelectTrigger className='w-36'>
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
                      </TableCell>
                      <TableCell>
                        {/* Zero keeps the built-in budget. A slow upstream can ask
                            for more time without changing anything else. */}
                        <Input
                          type='number'
                          min={0}
                          max={MAX_TIMEOUT_MINUTES}
                          value={target.timeoutMinutes || ''}
                          placeholder={String(DEFAULT_TIMEOUT_MINUTES)}
                          onChange={(event) => {
                            const parsed = Number(event.target.value);
                            updateTarget(index, {
                              timeoutMinutes:
                                Number.isFinite(parsed) && parsed > 0
                                  ? Math.min(parsed, MAX_TIMEOUT_MINUTES)
                                  : 0,
                            });
                          }}
                          disabled={readOnly}
                          className='w-24'
                        />
                      </TableCell>
                      <TableCell>
                        {/* Which check this row runs. Both benchmarks are asked of the
                            same model, so the choice belongs next to the model. */}
                        <Select
                          value={target.benchmark || INTELLIGENCE_BENCHMARKS[0]}
                          onValueChange={(value) =>
                            updateTarget(index, {
                              benchmark: value === INTELLIGENCE_BENCHMARKS[0] ? '' : value,
                            })
                          }
                          disabled={readOnly}
                        >
                          <SelectTrigger className='w-32'>
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
                      </TableCell>
                      <TableCell>
                        <div className='flex items-center gap-1'>
                          {/* Runs the saved target, not the row as edited: the
                              backend works from the stored configuration, so an
                              unsaved change would silently be ignored. */}
                          <Button
                            variant='outline'
                            size='sm'
                            className='h-7'
                            disabled={!canRun || readOnly || runNow.isPending || !isSavedTarget(index)}
                            onClick={() =>
                              runNow.mutate({ channelID: target.channelID, apiKey: target.apiKey })
                            }
                            aria-label={t('intelligence.settings.runThisTarget')}
                            data-testid='run-intelligence-target'
                          >
                            <IconPlayerPlay size={14} className='mr-1' />
                            {t('intelligence.settings.runThisTarget')}
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon'
                            onClick={() => removeTarget(index)}
                            disabled={readOnly}
                          >
                            <IconTrash size={16} />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
          <p className='text-muted-foreground text-xs'>{t('intelligence.settings.targetsHint')}</p>
        </div>

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
