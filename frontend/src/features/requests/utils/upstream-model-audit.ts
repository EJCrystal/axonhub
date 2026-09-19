interface ModelAuditExecution {
  modelID?: string | null;
  upstreamModelID?: string | null;
}

type ModelAuditStatus = 'matched' | 'mismatched' | 'unknown';

export function getUpstreamModelAudit(executions: readonly ModelAuditExecution[]) {
  const upstreamModelIds = new Set<string>();
  const mismatchedModelIds = new Set<string>();
  let unknownCount = 0;

  for (const execution of executions) {
    const sentModel = execution.modelID;
    const upstreamModel = execution.upstreamModelID;
    if (!sentModel?.trim() || !upstreamModel?.trim()) {
      unknownCount++;
      continue;
    }

    upstreamModelIds.add(upstreamModel);
    // Compare within this execution before deduplicating the display values.
    if (upstreamModel !== sentModel) {
      mismatchedModelIds.add(upstreamModel);
    }
  }

  const status: ModelAuditStatus =
    mismatchedModelIds.size > 0
      ? 'mismatched'
      : executions.length === 0 || unknownCount > 0
        ? 'unknown'
        : 'matched';

  return {
    status,
    upstreamModelIds: Array.from(upstreamModelIds),
    mismatchedModelIds: Array.from(mismatchedModelIds),
    unknownCount,
    comparedCount: executions.length - unknownCount,
  };
}
