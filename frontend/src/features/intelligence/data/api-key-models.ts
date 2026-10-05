// Resolves the models one channel API key can actually serve.
//
// Mirrors how routing decides: an explicit per-key assignment wins; otherwise
// the key is limited to the intersection of the channel's models and the
// models its upstream group has been observed to return. A key with no fetched
// capability data falls back to the channel list, since an absent list means
// "unknown", while an explicit empty list means "serves nothing".
export function modelsForAPIKey(
  credentials: {
    apiKeys?: string[] | null;
    apiKeyModels?: { apiKey: string; models: string[] }[] | null;
    apiKeyFetchedModels?: { apiKey: string; models: string[] }[] | null;
  } | null | undefined,
  supportedModels: string[],
  apiKey: string
): string[] {
  const channelModels = [...new Set(supportedModels)];
  if (!credentials) return channelModels;

  const explicit = credentials.apiKeyModels?.find((item) => item.apiKey === apiKey);
  if (explicit) return [...new Set(explicit.models)];

  const fetched = credentials.apiKeyFetchedModels?.find((item) => item.apiKey === apiKey);
  if (!fetched) return channelModels;

  const capability = new Set(fetched.models);
  return channelModels.filter((model) => capability.has(model));
}
