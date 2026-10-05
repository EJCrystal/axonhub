'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconPlus, IconTrash } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { useQueryChannels } from '@/features/channels/data/channels';
import { modelsForAPIKey } from '../data/api-key-models';
import { INTELLIGENCE_INTERVALS, IntelligenceConfig, IntelligenceInterval } from '../data/schema';
import { useSetIntelligenceConfig } from '../data/intelligence';

interface Props {
  config?: IntelligenceConfig;
  loading: boolean;
  readOnly: boolean;
}

interface DraftTarget {
  channelID: string;
  apiKey: string;
  modelID: string;
}

function maskKey(key: string): string {
  if (key.length <= 8) return '****';
  return key.slice(0, 4) + '****' + key.slice(-4);
}

export function IntelligenceSettings({ config, loading, readOnly }: Props) {
  const { t } = useTranslation();
  const save = useSetIntelligenceConfig();

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
    setTargets(config.targets.map((target) => ({ channelID: target.channelID, apiKey: target.apiKey ?? '', modelID: target.modelID })));
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

  const channelByID = (id: string) => channelOptions.find((channel) => channel.id === id);

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

  const addTarget = () => {
    const next = channelOptions.find((channel) => !targets.some((target) => target.channelID === channel.id));
    if (!next) return;
    const apiKey = keysFor(next.id)[0] ?? '';
    setTargets((prev) => [...prev, { channelID: next.id, apiKey, modelID: modelsFor(next.id, apiKey)[0] ?? '' }]);
  };

  const updateTarget = (index: number, patch: Partial<DraftTarget>) => {
    setTargets((prev) => prev.map((target, i) => (i === index ? { ...target, ...patch } : target)));
  };

  const removeTarget = (index: number) => {
    setTargets((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSave = () => {
    save.mutate({
      enabled,
      intervalMinutes,
      targets: targets.map((target) => ({ channelID: target.channelID, modelID: target.modelID, apiKey: target.apiKey })),
    });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('intelligence.settings.title')}</CardTitle>
        <CardDescription>{t('intelligence.settings.description')}</CardDescription>
      </CardHeader>
      <CardContent className='space-y-6'>
        <div className='flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4'>
          <div className='space-y-1'>
            <Label htmlFor='intelligence-enabled'>{t('intelligence.settings.enabled')}</Label>
            <p className='text-muted-foreground text-xs'>{t('intelligence.settings.enabledHint')}</p>
          </div>
          <Switch id='intelligence-enabled' checked={enabled} onCheckedChange={setEnabled} disabled={readOnly} />
        </div>

        <div className='space-y-2'>
          <Label>{t('intelligence.settings.interval')}</Label>
          <Select
            value={String(intervalMinutes)}
            onValueChange={(value) => setIntervalMinutes(Number(value) as IntelligenceInterval)}
            disabled={readOnly}
          >
            <SelectTrigger className='w-64'>
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
          <p className='text-muted-foreground text-xs'>{t('intelligence.settings.intervalHint')}</p>
        </div>

        <div className='space-y-2'>
          <div className='flex items-center justify-between'>
            <Label>{t('intelligence.settings.targets')}</Label>
            <Button variant='outline' size='sm' onClick={addTarget} disabled={readOnly || loading}>
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
                  <TableHead className='w-16'></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {targets.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4} className='text-muted-foreground text-center text-xs'>
                      {t('intelligence.settings.noTargets')}
                    </TableCell>
                  </TableRow>
                )}
                {targets.map((target, index) => {
                  const used = new Set(targets.filter((_, i) => i !== index).map((item) => item.channelID));
                  const keys = keysFor(target.channelID);
                  const models = modelsFor(target.channelID, target.apiKey);
                  return (
                    <TableRow key={`${target.channelID}-${index}`}>
                      <TableCell>
                        <Select
                          value={target.channelID}
                          onValueChange={(value) => {
                            const apiKey = keysFor(value)[0] ?? '';
                            updateTarget(index, { channelID: value, apiKey, modelID: modelsFor(value, apiKey)[0] ?? '' });
                          }}
                          disabled={readOnly}
                        >
                          <SelectTrigger className='w-44'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {channelOptions
                              .filter((channel) => !used.has(channel.id))
                              .map((channel) => (
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
                                <span className='font-mono text-xs'>{maskKey(key)}</span>
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
                        <Button variant='ghost' size='icon' onClick={() => removeTarget(index)} disabled={readOnly}>
                          <IconTrash size={16} />
                        </Button>
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
      </CardContent>
    </Card>
  );
}
