'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { IconClock, IconPlus, IconTrash } from '@tabler/icons-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Switch } from '@/components/ui/switch';
import { useGeneralSettings } from '@/features/system/data/system';
import { useChannelDetails, useUpdateChannelAPIKeyWorkTime, useUpdateChannelSettings } from '../data/channels';
import { Channel, OAUTH_CREDENTIAL_REF, WorkTimeSchedule, WorkTimeWindow } from '../data/schema';

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentRow: Channel;
}

interface KeyScheduleState {
  enabled: boolean;
  timezone: string;
  windows: WorkTimeWindow[];
}

const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7];
const COMMON_TIMEZONES = [
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Los_Angeles',
  'UTC',
];
const CLOCK_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;

function cloneWindows(windows: WorkTimeWindow[] | null | undefined): WorkTimeWindow[] {
  return (windows ?? []).map((window) => ({ days: [...window.days], start: window.start, end: window.end }));
}

function normalizeSchedule(schedule: WorkTimeSchedule | null | undefined): WorkTimeSchedule | null {
  if (!schedule || schedule.windows.length === 0) {
    return null;
  }
  return {
    timezone: schedule.timezone ?? '',
    windows: schedule.windows.map((window) => ({ days: [...window.days].sort(), start: window.start, end: window.end })),
  };
}

function windowsAreValid(windows: WorkTimeWindow[]): boolean {
  return windows.every(
    (window) =>
      window.days.length > 0 &&
      CLOCK_PATTERN.test(window.start) &&
      CLOCK_PATTERN.test(window.end) &&
      window.start !== window.end
  );
}

function maskKey(key: string): string {
  if (key === OAUTH_CREDENTIAL_REF) {
    return key;
  }
  if (key.length <= 8) {
    return '****';
  }
  return key.slice(0, 4) + '****' + key.slice(-4);
}

interface WindowsEditorProps {
  windows: WorkTimeWindow[];
  onChange: (windows: WorkTimeWindow[]) => void;
}

function WindowsEditor({ windows, onChange }: WindowsEditorProps) {
  const { t } = useTranslation();

  const updateWindow = (index: number, patch: Partial<WorkTimeWindow>) => {
    onChange(windows.map((window, i) => (i === index ? { ...window, ...patch } : window)));
  };

  const toggleDay = (index: number, day: number) => {
    const window = windows[index];
    const days = window.days.includes(day) ? window.days.filter((value) => value !== day) : [...window.days, day].sort();
    updateWindow(index, { days });
  };

  return (
    <div className='space-y-3'>
      {windows.length === 0 && <p className='text-muted-foreground text-sm'>{t('channels.dialogs.workTime.empty')}</p>}
      {windows.map((window, index) => (
        <div key={index} className='space-y-2 rounded-md border p-3'>
          <div className='flex flex-wrap items-center gap-1'>
            {WEEKDAYS.map((day) => {
              const active = window.days.includes(day);
              return (
                <Button
                  key={day}
                  type='button'
                  size='sm'
                  variant={active ? 'default' : 'outline'}
                  className='h-7 px-2 text-xs'
                  onClick={() => toggleDay(index, day)}
                >
                  {t('channels.dialogs.workTime.weekdays.' + day)}
                </Button>
              );
            })}
            <Button
              type='button'
              size='sm'
              variant='ghost'
              className='text-destructive ml-auto h-7 px-2'
              onClick={() => onChange(windows.filter((_, i) => i !== index))}
            >
              <IconTrash className='h-3.5 w-3.5' />
            </Button>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              type='time'
              value={window.start}
              onChange={(event) => updateWindow(index, { start: event.target.value })}
              className='w-32'
            />
            <span className='text-muted-foreground text-sm'>-</span>
            <Input
              type='time'
              value={window.end}
              onChange={(event) => updateWindow(index, { end: event.target.value })}
              className='w-32'
            />
            <span className='text-muted-foreground text-xs'>{t('channels.dialogs.workTime.crossMidnightHint')}</span>
          </div>
        </div>
      ))}
      <Button
        type='button'
        size='sm'
        variant='outline'
        onClick={() => onChange([...windows, { days: [1, 2, 3, 4, 5], start: '09:00', end: '18:00' }])}
      >
        <IconPlus className='mr-1 h-3.5 w-3.5' />
        {t('channels.dialogs.workTime.addWindow')}
      </Button>
    </div>
  );
}

interface TimezoneInputProps {
  value: string;
  onChange: (value: string) => void;
  id: string;
  placeholder?: string;
}

function TimezoneInput({ value, onChange, id, placeholder }: TimezoneInputProps) {
  const { t } = useTranslation();

  return (
    <div className='space-y-1'>
      <Label htmlFor={id} className='text-sm'>
        {t('channels.dialogs.workTime.timezone')}
      </Label>
      <Input
        id={id}
        list={id + '-options'}
        value={value}
        placeholder={placeholder || 'Asia/Shanghai'}
        onChange={(event) => onChange(event.target.value)}
        className='w-64'
      />
      <datalist id={id + '-options'}>
        {COMMON_TIMEZONES.map((timezone) => (
          <option key={timezone} value={timezone} />
        ))}
      </datalist>
      <p className='text-muted-foreground text-xs'>{t('channels.dialogs.workTime.timezoneHint')}</p>
    </div>
  );
}

export function ChannelsWorkTimeDialog({ open, onOpenChange, currentRow }: Props) {
  const { t } = useTranslation();
  const { data: details, isLoading } = useChannelDetails(currentRow.id, { enabled: open });
  const { data: generalSettings } = useGeneralSettings();
  const systemTimezone = generalSettings?.timezone ?? '';
  const updateChannelSettings = useUpdateChannelSettings();
  const updateKeyWorkTime = useUpdateChannelAPIKeyWorkTime();

  const [channelTimezone, setChannelTimezone] = useState('');
  const [channelWindows, setChannelWindows] = useState<WorkTimeWindow[]>([]);
  const [keyStates, setKeyStates] = useState<Record<string, KeyScheduleState>>({});
  const initialRef = useRef<{ channel: string; keys: Record<string, string> }>({ channel: '', keys: {} });
  const [saving, setSaving] = useState(false);

  const source = details ?? currentRow;

  useEffect(() => {
    if (!open) {
      return;
    }
    if (!details) {
      return;
    }
    if (initialRef.current.channel === currentRow.id) {
      return;
    }

    const workTime = normalizeSchedule(details.settings?.workTime);
    setChannelTimezone(workTime?.timezone ?? '');
    setChannelWindows(cloneWindows(workTime?.windows));

    const next: Record<string, KeyScheduleState> = {};
    const initialKeys: Record<string, string> = {};
    const configured = details.apiKeyWorkTimes ?? [];
    for (const item of configured) {
      const schedule = normalizeSchedule(item.workTime);
      if (!schedule) {
        continue;
      }
      next[item.key] = { enabled: true, timezone: schedule.timezone ?? '', windows: cloneWindows(schedule.windows) };
      initialKeys[item.key] = JSON.stringify(schedule);
    }
    setKeyStates(next);
    initialRef.current = { channel: currentRow.id, keys: initialKeys };
  }, [currentRow.id, details, open, systemTimezone]);

  useEffect(() => {
    if (open) {
      return;
    }
    initialRef.current = { channel: '', keys: {} };
  }, [open]);

  const keys = useMemo(() => {
    const refs = new Set<string>();
    for (const key of source.credentials?.apiKeys ?? []) {
      refs.add(key);
    }
    for (const item of source.apiKeyWorkTimes ?? []) {
      refs.add(item.key);
    }
    refs.delete(OAUTH_CREDENTIAL_REF);

    return Array.from(refs);
  }, [source]);

  const channelSchedule = useMemo(() => normalizeSchedule({ timezone: channelTimezone, windows: channelWindows }), [channelTimezone, channelWindows]);

  const handleToggleKey = (key: string, enabled: boolean) => {
    setKeyStates((prev) => ({
      ...prev,
      [key]: {
        enabled,
        timezone: prev[key]?.timezone || channelTimezone,
        windows: prev[key]?.windows?.length ? prev[key].windows : [{ days: [1, 2, 3, 4, 5], start: '09:00', end: '18:00' }],
      },
    }));
  };

  const handleKeyChange = (key: string, patch: Partial<KeyScheduleState>) => {
    setKeyStates((prev) => ({
      ...prev,
      [key]: {
        enabled: prev[key]?.enabled ?? true,
        timezone: prev[key]?.timezone ?? '',
        windows: prev[key]?.windows ?? [],
        ...patch,
      },
    }));
  };

  const handleSave = async () => {
    if (!windowsAreValid(channelWindows)) {
      toast.error(t('channels.dialogs.workTime.invalidWindows'));
      return;
    }
    for (const key of keys) {
      const state = keyStates[key];
      if (state?.enabled && !windowsAreValid(state.windows)) {
        toast.error(t('channels.dialogs.workTime.invalidKeyWindows', { key: maskKey(key) }));
        return;
      }
    }

    setSaving(true);
    try {
      await updateChannelSettings.mutateAsync({
        id: currentRow.id,
        patch: { workTime: channelSchedule },
      });

      for (const key of keys) {
        const state = keyStates[key];
        const next = state?.enabled ? normalizeSchedule({ timezone: state.timezone, windows: state.windows }) : null;
        if (JSON.stringify(next) === (initialRef.current.keys[key] ?? 'null')) {
          continue;
        }
        await updateKeyWorkTime.mutateAsync({ channelID: currentRow.id, key, workTime: next });
      }

      toast.success(t('channels.messages.updateSuccess'));
      onOpenChange(false);
    } catch {
      // mutation hooks report the error
    } finally {
      setSaving(false);
    }
  };

  const outsideWorkTime = source.workTimeActive === false;
  const nextStart = source.nextWorkTimeStart;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-hidden sm:max-w-3xl'>
        <DialogHeader className='text-left'>
          <DialogTitle className='flex items-center gap-2'>
            <IconClock className='h-5 w-5' />
            {t('channels.dialogs.workTime.title')}
          </DialogTitle>
          <DialogDescription>{t('channels.dialogs.workTime.description', { name: currentRow.name })}</DialogDescription>
        </DialogHeader>

        <div className='flex items-center gap-2'>
          {outsideWorkTime ? (
            <Badge variant='outline' className='text-amber-600'>
              {t('channels.dialogs.workTime.status.closed')}
            </Badge>
          ) : (
            <Badge variant='outline' className='text-emerald-600'>
              {t('channels.dialogs.workTime.status.open')}
            </Badge>
          )}
          {nextStart && (
            <span className='text-muted-foreground text-xs'>{t('channels.dialogs.workTime.nextStart', { time: new Date(nextStart).toLocaleString() })}</span>
          )}
        </div>

        <ScrollArea className='max-h-[60vh] pr-3'>
          <div className='space-y-4'>
            <Card>
              <CardHeader className='pb-3'>
                <CardTitle className='text-base'>{t('channels.dialogs.workTime.channel.title')}</CardTitle>
                <CardDescription>{t('channels.dialogs.workTime.channel.description')}</CardDescription>
              </CardHeader>
              <CardContent className='space-y-3'>
                <TimezoneInput
                  id='channel-work-time-timezone'
                  value={channelTimezone}
                  onChange={setChannelTimezone}
                  placeholder={systemTimezone || 'Asia/Shanghai'}
                />
                <WindowsEditor windows={channelWindows} onChange={setChannelWindows} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader className='pb-3'>
                <CardTitle className='text-base'>{t('channels.dialogs.workTime.keys.title')}</CardTitle>
                <CardDescription>{t('channels.dialogs.workTime.keys.description')}</CardDescription>
              </CardHeader>
              <CardContent className='space-y-3'>
                {isLoading && keys.length === 0 && <p className='text-muted-foreground text-sm'>{t('common.loading')}</p>}
                {!isLoading && keys.length === 0 && <p className='text-muted-foreground text-sm'>{t('channels.dialogs.workTime.keys.empty')}</p>}
                {keys.map((key) => {
                  const state = keyStates[key];
                  const enabled = state?.enabled ?? false;
                  return (
                    <div key={key} className='space-y-3 rounded-md border p-3'>
                      <div className='flex items-center justify-between gap-2'>
                        <div className='flex items-center gap-2'>
                          <span className='font-mono text-sm'>{maskKey(key)}</span>
                          {enabled && <Badge variant='secondary'>{t('channels.dialogs.workTime.keys.limited')}</Badge>}
                        </div>
                        <div className='flex items-center gap-2'>
                          <Label htmlFor={'work-time-key-' + key} className='text-muted-foreground text-xs'>
                            {t('channels.dialogs.workTime.keys.enable')}
                          </Label>
                          <Switch
                            id={'work-time-key-' + key}
                            checked={enabled}
                            onCheckedChange={(checked) => handleToggleKey(key, checked)}
                          />
                        </div>
                      </div>
                      {enabled && (
                        <div className='space-y-3'>
                          <TimezoneInput
                            id={'work-time-key-timezone-' + key}
                            value={state?.timezone ?? ''}
                            onChange={(value) => handleKeyChange(key, { timezone: value })}
                            placeholder={systemTimezone || 'Asia/Shanghai'}
                          />
                          <WindowsEditor
                            windows={state?.windows ?? []}
                            onChange={(windows) => handleKeyChange(key, { windows })}
                          />
                        </div>
                      )}
                    </div>
                  );
                })}
              </CardContent>
            </Card>
          </div>
        </ScrollArea>

        <DialogFooter className='flex items-center justify-between sm:justify-between'>
          <p className='text-muted-foreground text-xs'>{t('channels.dialogs.workTime.footer')}</p>
          <div className='flex gap-2'>
            <Button variant='outline' onClick={() => onOpenChange(false)} disabled={saving}>
              {t('common.buttons.cancel')}
            </Button>
            <Button onClick={handleSave} disabled={saving || isLoading || !details}>
              {saving ? t('common.buttons.saving') : t('common.buttons.save')}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
