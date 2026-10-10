'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { IconChevronDown } from '@tabler/icons-react';
import { sameChannelId } from '../data/channel-id';
import { maskAPIKey } from '../data/mask-key';
import { INTELLIGENCE_BENCHMARKS, REASONING_EFFORT_OPTIONS } from '../data/benchmarks';

// NO_EFFORT mirrors the settings table: Radix Select reserves the empty string,
// so "use the provider default" needs a value of its own.
const NO_EFFORT = '__default__';

interface ChannelOption {
  id: string;
  name: string;
  keys: string[];
  modelsFor: (apiKey: string) => string[];
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channels: ChannelOption[];
  // A channel may be configured once per key, so the dialog must not offer a
  // pair that is already in the table.
  isPairConfigured: (channelID: string, apiKey: string) => boolean;
  onAdd: (target: {
    channelID: string;
    apiKey: string;
    modelID: string;
    reasoningEffort: string;
    benchmark: string;
  }) => void;
}

// IntelligenceAddTargetDialog collects one target before it joins the table.
//
// Adding straight into the table meant every new row started on the first free
// channel and key, leaving the reader to fix channel, key, model and check by
// hand. Choosing them here matches how a channel is added elsewhere, and makes
// the available keys visible at the moment of the choice.
export function IntelligenceAddTargetDialog({ open, onOpenChange, channels, isPairConfigured, onAdd }: Props) {
  const { t } = useTranslation();

  const [channelID, setChannelID] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [modelID, setModelID] = useState('');
  const [reasoningEffort, setReasoningEffort] = useState('');
  const [benchmark, setBenchmark] = useState('');
  const [channelPickerOpen, setChannelPickerOpen] = useState(false);

  const channel = useMemo(
    () => channels.find((option) => sameChannelId(option.id, channelID)),
    [channels, channelID]
  );

  // Only the pairs that are still free, so the list cannot propose a duplicate.
  const keys = useMemo(
    () => (channel ? channel.keys.filter((key) => !isPairConfigured(channel.id, key)) : []),
    [channel, isPairConfigured]
  );

  const models = useMemo(
    () => (channel && apiKey ? channel.modelsFor(apiKey) : []),
    [channel, apiKey]
  );

  // Start each visit from a clean, valid selection: the first channel with a
  // free key, its first free key, and that key's first model.
  useEffect(() => {
    if (!open) return;

    const firstChannel = channels.find((option) =>
      option.keys.some((key) => !isPairConfigured(option.id, key))
    );
    if (!firstChannel) {
      setChannelID('');
      return;
    }

    const firstKey = firstChannel.keys.find((key) => !isPairConfigured(firstChannel.id, key)) ?? '';
    setChannelID(firstChannel.id);
    setApiKey(firstKey);
    setModelID(firstChannel.modelsFor(firstKey)[0] ?? '');
    setReasoningEffort('');
    setBenchmark('');
  }, [open, channels, isPairConfigured]);

  const canAdd = Boolean(channelID && apiKey && modelID) && !isPairConfigured(channelID, apiKey);

  const submit = () => {
    if (!canAdd) return;

    onAdd({ channelID, apiKey, modelID, reasoningEffort, benchmark });
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('intelligence.settings.addDialog.title')}</DialogTitle>
          <DialogDescription>{t('intelligence.settings.addDialog.description')}</DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-2'>
          <div className='space-y-2'>
            <Label>{t('intelligence.settings.channelColumn')}</Label>
            {/* The channel list is longer than a dropdown can usefully show, so
                it is searchable: type part of a name to narrow it down. */}
            <Popover open={channelPickerOpen} onOpenChange={setChannelPickerOpen}>
              <PopoverTrigger asChild>
                <Button
                  variant='outline'
                  role='combobox'
                  aria-expanded={channelPickerOpen}
                  data-testid='add-target-channel'
                  className='w-full justify-between font-normal'
                >
                  <span className={channel ? '' : 'text-muted-foreground'}>
                    {channel?.name ?? t('intelligence.settings.addDialog.channelPlaceholder')}
                  </span>
                  <IconChevronDown size={16} className='opacity-50' />
                </Button>
              </PopoverTrigger>
              <PopoverContent className='w-[--radix-popover-trigger-width] p-0' align='start'>
                <Command>
                  <CommandInput placeholder={t('intelligence.settings.addDialog.channelSearch')} />
                  <CommandList>
                    <CommandEmpty>{t('intelligence.settings.addDialog.channelEmpty')}</CommandEmpty>
                    <CommandGroup>
                      {channels.map((option) => (
                        <CommandItem
                          key={option.id}
                          value={option.name}
                          onSelect={() => {
                            const nextKey = option.keys.find((key) => !isPairConfigured(option.id, key)) ?? '';
                            setChannelID(option.id);
                            setApiKey(nextKey);
                            setModelID(option.modelsFor(nextKey)[0] ?? '');
                            setChannelPickerOpen(false);
                          }}
                        >
                          {option.name}
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
          </div>

          <div className='space-y-2'>
            <Label>{t('intelligence.settings.keyColumn')}</Label>
            <Select
              value={apiKey}
              onValueChange={(value) => {
                setApiKey(value);
                setModelID(channel?.modelsFor(value)[0] ?? '');
              }}
              disabled={keys.length === 0}
            >
              <SelectTrigger data-testid='add-target-key'>
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
          </div>

          <div className='space-y-2'>
            <Label>{t('intelligence.settings.modelColumn')}</Label>
            <Select value={modelID} onValueChange={setModelID} disabled={models.length === 0}>
              <SelectTrigger data-testid='add-target-model'>
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
          </div>

          <div className='grid grid-cols-2 gap-4'>
            <div className='space-y-2'>
              <Label>{t('intelligence.settings.effortColumn')}</Label>
              <Select
                value={reasoningEffort || NO_EFFORT}
                onValueChange={(value) => setReasoningEffort(value === NO_EFFORT ? '' : value)}
              >
                <SelectTrigger data-testid='add-target-effort'>
                  <SelectValue />
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
            </div>

            <div className='space-y-2'>
              <Label>{t('intelligence.settings.benchmarkColumn')}</Label>
              <Select
                value={benchmark || INTELLIGENCE_BENCHMARKS[0]}
                onValueChange={(value) => setBenchmark(value === INTELLIGENCE_BENCHMARKS[0] ? '' : value)}
              >
                <SelectTrigger data-testid='add-target-benchmark'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {INTELLIGENCE_BENCHMARKS.map((value) => (
                    <SelectItem key={value} value={value}>
                      {value === 'candy'
                        ? t('intelligence.settings.benchmarkCandy')
                        : t('intelligence.settings.benchmarkPelican')}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {keys.length === 0 && (
            <p className='text-muted-foreground text-xs'>{t('intelligence.settings.addDialog.noFreeKey')}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('intelligence.settings.addDialog.cancel')}
          </Button>
          <Button onClick={submit} disabled={!canAdd} data-testid='add-target-confirm'>
            {t('intelligence.settings.addDialog.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
