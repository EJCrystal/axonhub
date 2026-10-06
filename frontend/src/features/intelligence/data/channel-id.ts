// Channel ids are GIDs of the form gid://axonhub/<Type>/<id>. The type segment
// is capitalized by the ent layer ("Channel"), but a configuration written by an
// earlier build stored it lowercase, so comparing the whole string silently
// fails to match a saved target against the channel list and the row renders
// empty. Comparing the numeric id keeps both spellings working.
export function channelIdKey(id: string): string {
  return id.split('/').pop() ?? id;
}

export function sameChannelId(left: string, right: string): boolean {
  return channelIdKey(left) === channelIdKey(right);
}
