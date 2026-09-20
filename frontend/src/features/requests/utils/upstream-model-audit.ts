interface ModelAuditExecution {
  status?: 'pending' | 'processing' | 'completed' | 'failed' | 'canceled';
  modelID?: string | null;
  outboundModelID?: string | null;
  upstreamModelID?: string | null;
  upstreamModelIds?: readonly string[] | null;
}

type ModelAuditStatus = 'matched' | 'mismatched' | 'unknown' | 'conflicting';

// Detail-page audit. List rows use the backend's full execution-set summary.
export function getUpstreamModelAudit(executions: readonly ModelAuditExecution[]) {
  const upstreamModelIds = new Set<string>();
  const mismatchedModelIds = new Set<string>();
  const conflictingModelIds = new Set<string>();
  let unknownCount = 0;
  let conflictCount = 0;
  let hasCompletedComparison = false;
  let blockingUnknownCount = 0;

  for (const execution of executions) {
    // Routing modelID is not evidence of what was sent after body overrides.
    const sentModel = execution.outboundModelID;
    const reportedModels = Array.from(
      new Set([execution.upstreamModelID, ...(execution.upstreamModelIds ?? [])].filter((model): model is string => Boolean(model?.trim())))
    );
    for (const model of reportedModels) upstreamModelIds.add(model);
    if (reportedModels.length > 1) {
      conflictCount++;
      for (const model of reportedModels) conflictingModelIds.add(model);
    }
    if (!sentModel?.trim() || reportedModels.length === 0) {
      unknownCount++;
      if (execution.status !== 'failed' && execution.status !== 'canceled') blockingUnknownCount++;
      continue;
    }
    hasCompletedComparison ||= execution.status === 'completed';

    // Compare within this execution before deduplicating the display values.
    for (const model of reportedModels) {
      if (model !== sentModel) mismatchedModelIds.add(model);
    }
  }

  const status: ModelAuditStatus =
    conflictCount > 0
      ? 'conflicting'
      : mismatchedModelIds.size > 0
        ? 'mismatched'
        : executions.length === 0 || (unknownCount > 0 && (!hasCompletedComparison || blockingUnknownCount > 0))
          ? 'unknown'
          : 'matched';

  return {
    status,
    upstreamModelIds: Array.from(upstreamModelIds),
    mismatchedModelIds: Array.from(mismatchedModelIds),
    conflictingModelIds: Array.from(conflictingModelIds),
    unknownCount,
    comparedCount: executions.length - unknownCount,
    conflictCount,
  };
}
