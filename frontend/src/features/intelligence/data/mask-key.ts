// Mirrors the backend's maskAPIKey, which is what a stored result's keyPrefix
// contains. The settings and filter pickers have real keys in hand and need to
// match them against that prefix, so both sides must agree on the shape.
export function maskAPIKey(key: string): string {
  if (key.length <= 8) return '****';
  return key.slice(0, 4) + '****' + key.slice(-4);
}
