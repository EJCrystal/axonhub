import { ChannelCredentials } from '@/features/channels/data/schema';
import { maskAPIKey } from './mask-key';

export interface ChannelAPIKeyEntry {
  key: string;
  remark: string;
}

// channelAPIKeyEntries pairs each key of a channel with its remark. The channel
// stores them positionally, so apiKeyRemarks[i] belongs to apiKeys[i]; a
// single-key channel keeps its key in apiKey instead, and a configuration saved
// before remarks existed carries no array at all.
export function channelAPIKeyEntries(credentials: ChannelCredentials | null | undefined): ChannelAPIKeyEntry[] {
  const keys = [...(credentials?.apiKeys ?? [])];
  const remarks = [...(credentials?.apiKeyRemarks ?? [])];

  const single = credentials?.apiKey;
  if (single && !keys.includes(single)) {
    keys.unshift(single);
    remarks.unshift('');
  }

  return keys
    .map((key, index) => ({ key, remark: (remarks[index] ?? '').trim() }))
    .filter((entry) => entry.key.trim() !== '');
}

// apiKeyRemark answers the remark stored for one key. A key without a remark
// answers '', which callers read as "nothing to show" rather than as an empty
// tooltip.
export function apiKeyRemark(credentials: ChannelCredentials | null | undefined, apiKey: string): string {
  return channelAPIKeyEntries(credentials).find((entry) => entry.key === apiKey)?.remark ?? '';
}

// maskedKeyRemarks maps each masked key to its remark. History records keep only
// the masked prefix of the key a run exercised, so that prefix is what a record
// can be joined back to the channel's credentials on.
export function maskedKeyRemarks(credentials: ChannelCredentials | null | undefined): Map<string, string> {
  const byMasked = new Map<string, string>();

  for (const entry of channelAPIKeyEntries(credentials)) {
    if (entry.remark) byMasked.set(maskAPIKey(entry.key), entry.remark);
  }

  return byMasked;
}
