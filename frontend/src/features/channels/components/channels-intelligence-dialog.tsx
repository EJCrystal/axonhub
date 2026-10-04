'use client';

import { useEffect, useMemo, useState } from 'react';
import { IconBrain, IconPlayerPlay } from '@tabler/icons-react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Textarea } from '@/components/ui/textarea';
import { ErrorDisplay } from '../utils/error-formatter';
import { useEvaluateChannelIntelligence } from '../data/channels';
import { IntelligenceHTMLPreview } from './channels-intelligence-preview';
import { intelligenceVerdict, verdictBadgeVariant } from './intelligence-verdict';
import { Channel, IntelligenceKeyResult } from '../data/schema';

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channel: Channel;
}

// Masks an API key the same way the backend does, so the dialog can match
// returned results back to the local key list.
function maskKey(key: string): string {
  if (key.length <= 8) return '****';
  return key.slice(0, 4) + '****' + key.slice(-4);
}

export function ChannelsIntelligenceDialog({ open, onOpenChange, channel }: Props) {
  const { t } = useTranslation();
  const evaluate = useEvaluateChannelIntelligence();

  const [model, setModel] = useState(channel.defaultTestModel || '');
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);
  const [prompt, setPrompt] = useState('');
  const [results, setResults] = useState<IntelligenceKeyResult[] | null>(null);

  const keys = useMemo(() => {
    const all = [...(channel.credentials?.apiKeys ?? [])];
    const primary = channel.credentials?.apiKey;
    if (primary && !all.includes(primary)) {
      all.unshift(primary);
    }
    return all.filter((key) => key.trim().length > 0);
  }, [channel.credentials?.apiKey, channel.credentials?.apiKeys]);

  const disabledKeys = useMemo(() => new Set((channel.disabledAPIKeys ?? []).map((item) => item.key)), [channel.disabledAPIKeys]);

  useEffect(() => {
    if (!open) return;
    setModel(channel.defaultTestModel || channel.supportedModels[0] || '');
    setSelectedKeys(keys.filter((key) => !disabledKeys.has(key)));
    setPrompt('');
    setResults(null);
  }, [open, channel.id, channel.defaultTestModel, channel.supportedModels, keys, disabledKeys]);

  const toggleKey = (key: string, checked: boolean) => {
    setSelectedKeys((prev) => (checked ? [...prev, key] : prev.filter((item) => item !== key)));
  };

  const handleRun = async () => {
    setResults(null);

    const payload = await evaluate.mutateAsync({
      channelID: channel.id,
      modelID: model || undefined,
      keys: selectedKeys.length > 0 ? selectedKeys : undefined,
      prompt: prompt.trim() ? prompt : undefined,
    });

    setResults(payload.results);
  };

  const resultFor = (key: string) => results?.find((item) => item.keyPrefix === maskKey(key));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='flex max-h-[90vh] flex-col w-full max-w-full sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2 text-lg sm:text-xl'>
            <IconBrain size={20} />
            {t('channels.dialogs.intelligence.title')}
          </DialogTitle>
          <DialogDescription>{t('channels.dialogs.intelligence.description', { name: channel.name })}</DialogDescription>
        </DialogHeader>

        <div className='min-h-0 flex-1 space-y-4 overflow-y-auto'>
          <div className='space-y-2'>
            <Label htmlFor='intelligence-model'>{t('channels.dialogs.intelligence.modelLabel')}</Label>
            <Select value={model} onValueChange={setModel}>
              <SelectTrigger id='intelligence-model' className='w-full'>
                <SelectValue placeholder={t('channels.dialogs.intelligence.modelPlaceholder')} />
              </SelectTrigger>
              <SelectContent>
                {channel.supportedModels.map((item) => (
                  <SelectItem key={item} value={item}>
                    {item}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className='space-y-2'>
            <Label>{t('channels.dialogs.intelligence.keysLabel')}</Label>
            <div className='rounded-lg border'>
              <div className='max-h-48 overflow-auto'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className='w-12'></TableHead>
                      <TableHead>{t('channels.dialogs.intelligence.keyColumn')}</TableHead>
                      <TableHead className='w-28'>{t('channels.dialogs.intelligence.statusColumn')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {keys.map((key) => {
                      const disabled = disabledKeys.has(key);
                      const result = resultFor(key);
                      const masked = maskKey(key);
                      return (
                        <TableRow key={key}>
                          <TableCell>
                            <Checkbox
                              checked={selectedKeys.includes(key)}
                              disabled={disabled}
                              onCheckedChange={(checked) => toggleKey(key, !!checked)}
                            />
                          </TableCell>
                          <TableCell className='font-mono text-xs'>{masked}</TableCell>
                          <TableCell>
                            {disabled ? (
                              <Badge variant='secondary'>{t('channels.dialogs.intelligence.keyDisabled')}</Badge>
                            ) : result ? (
                              <Badge variant={verdictBadgeVariant(intelligenceVerdict(result))}>
                                {t(`channels.dialogs.intelligence.verdict.${intelligenceVerdict(result)}`)}
                              </Badge>
                            ) : (
                              <span className='text-muted-foreground text-xs'>{t('channels.dialogs.intelligence.keyNotRun')}</span>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            </div>
            <p className='text-muted-foreground text-xs'>{t('channels.dialogs.intelligence.keysHint')}</p>
            {keys.length === 0 && <p className='text-muted-foreground text-xs'>{t('channels.dialogs.intelligence.noKeysHint')}</p>}
          </div>

          <div className='space-y-2'>
            <Label htmlFor='intelligence-prompt'>{t('channels.dialogs.intelligence.promptLabel')}</Label>
            <Textarea
              id='intelligence-prompt'
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              placeholder={t('channels.dialogs.intelligence.promptPlaceholder')}
              className='min-h-20'
            />
            <p className='text-muted-foreground text-xs'>{t('channels.dialogs.intelligence.promptHint')}</p>
          </div>

          {results && (
            <div className='space-y-2'>
              {results.map((item) => (
                <div key={item.keyPrefix} className='rounded-lg border p-3 text-xs'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-mono'>{item.keyPrefix}</span>
                    <Badge variant={verdictBadgeVariant(intelligenceVerdict(item))}>
                      {t(`channels.dialogs.intelligence.verdict.${intelligenceVerdict(item)}`)}
                    </Badge>
                    {item.taskID && <span className='text-muted-foreground'>{t('channels.dialogs.intelligence.taskID', { id: item.taskID })}</span>}
                  </div>
                  {item.reason && <p className='mt-2'>{item.reason}</p>}
                  {item.durationMs > 0 && (
                    <p className='text-muted-foreground mt-2'>
                      {t('channels.dialogs.intelligence.duration', { seconds: (item.durationMs / 1000).toFixed(1) })}
                      {item.generationMs > 0 &&
                        ` · ${t('channels.dialogs.intelligence.generation', { seconds: (item.generationMs / 1000).toFixed(1) })}`}
                    </p>
                  )}
                  {item.error && <ErrorDisplay error={item.error} messageClassName='mt-2 text-xs text-red-600' />}
                  <IntelligenceHTMLPreview html={item.html} verdict={intelligenceVerdict(item)} />
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter className='gap-2 sm:justify-between'>
          <p className='text-muted-foreground text-xs'>{t('channels.dialogs.intelligence.footerHint')}</p>
          <Button
            onClick={handleRun}
            disabled={evaluate.isPending || !model || (keys.length > 0 && selectedKeys.length === 0)}
            data-testid='run-intelligence-button'
          >
            <IconPlayerPlay className='mr-1 h-4 w-4' />
            {evaluate.isPending ? t('channels.dialogs.intelligence.running') : t('channels.dialogs.intelligence.run')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
