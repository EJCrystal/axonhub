import type { TFunction } from 'i18next';

interface ModelAuditExecution {
  status?: 'pending' | 'processing' | 'completed' | 'failed' | 'canceled';
  modelID?: string | null;
  outboundModelID?: string | null;
  upstreamModelID?: string | null;
  upstreamModelIds?: readonly string[] | null;
}

type ModelAuditStatus = 'matched' | 'mismatched' | 'unknown' | 'conflicting';

export type ModelAuditVerdictTone = 'success' | 'danger' | 'pending' | 'muted';

export interface ModelAuditVerdict {
  tone: ModelAuditVerdictTone;
  message: string;
}

export const MODEL_AUDIT_VERDICT_CLASS: Record<ModelAuditVerdictTone, string> = {
  success: 'font-mono text-xs text-emerald-600 dark:text-emerald-400',
  danger: 'text-xs font-medium text-destructive',
  pending: 'text-xs font-medium text-sky-700 dark:text-sky-300',
  muted: 'text-muted-foreground text-xs',
};

// The list reads the backend's execution-set summary, which carries no per-verdict
// evidence. Only the detail page computes equalModelIds from the raw executions.
type ModelAuditSummary = Omit<ReturnType<typeof getUpstreamModelAudit>, 'equalModelIds'>;

// Detail-page audit. List rows use the backend's full execution-set summary.
export function getUpstreamModelAudit(executions: readonly ModelAuditExecution[]) {
  const matchedUpstreamIds = new Set<string>();
  const equalModelIds = new Set<string>();
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
      else {
        equalModelIds.add(model);
        if (execution.status === 'completed') matchedUpstreamIds.add(model);
      }
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
    matchedUpstreamIds: Array.from(matchedUpstreamIds),
    equalModelIds: Array.from(equalModelIds),
    upstreamModelIds: Array.from(upstreamModelIds),
    mismatchedModelIds: Array.from(mismatchedModelIds),
    conflictingModelIds: Array.from(conflictingModelIds),
    unknownCount,
    comparedCount: executions.length - unknownCount,
    conflictCount,
  };
}

// List tooltips use the complete backend summary, never the paginated executions.
export function getRequestModelAuditTooltip(modelAudit: ModelAuditSummary, requestStatus: ModelAuditExecution['status'], t: TFunction) {
  if (requestStatus === 'pending' || requestStatus === 'processing') return t('requests.tooltips.upstreamModelRequestProcessing');
  if (requestStatus === 'failed' || requestStatus === 'canceled') return t('requests.tooltips.upstreamModelRequestFailed');

  if (modelAudit.status === 'matched') {
    // Missing successful evidence must not fall back to failed retry names.
    if (modelAudit.matchedUpstreamIds.length === 0) return t('requests.tooltips.upstreamModelUnknown');
    return t(
      modelAudit.unknownCount > 0 ? 'requests.tooltips.upstreamModelMatchedAfterRetries' : 'requests.tooltips.upstreamModelMatching',
      { model: modelAudit.matchedUpstreamIds.join(', '), unknown: modelAudit.unknownCount }
    );
  }

  let tooltip = t('requests.tooltips.upstreamModelUnknown');
  if (modelAudit.status === 'mismatched') {
    tooltip = t('requests.tooltips.upstreamModelMismatch', { model: modelAudit.mismatchedModelIds.join(', ') });
  } else if (modelAudit.status === 'conflicting') {
    tooltip = t('requests.tooltips.upstreamModelConflict', { model: modelAudit.conflictingModelIds.join(', ') });
  }
  if (modelAudit.unknownCount > 0 && modelAudit.comparedCount > 0) {
    const partial = t('requests.tooltips.upstreamModelPartial', { compared: modelAudit.comparedCount, unknown: modelAudit.unknownCount });
    return modelAudit.status === 'unknown' ? partial : `${tooltip} ${partial}`;
  }
  return tooltip;
}

// One verdict per execution row. Lifecycle decides the tone, so a failed retry
// that happens to match can never render as a green success conclusion.
export function getExecutionModelAuditVerdict(
  modelAudit: ReturnType<typeof getUpstreamModelAudit>,
  executionStatus: ModelAuditExecution['status'],
  t: TFunction
): ModelAuditVerdict {
  if (executionStatus === 'pending' || executionStatus === 'processing') {
    return { tone: 'pending', message: t('requests.tooltips.upstreamModelRequestProcessing') };
  }

  // Known anomalies outrank the lifecycle: a mismatch or stream conflict is a
  // real finding whether or not the execution succeeded.
  if (modelAudit.status === 'conflicting') {
    return { tone: 'danger', message: t('requests.detail.upstreamModelConflict') };
  }
  if (modelAudit.status === 'mismatched') {
    return {
      tone: 'danger',
      message: t('requests.detail.upstreamModelMismatch', { model: modelAudit.mismatchedModelIds.join(', ') }),
    };
  }

  const failed = executionStatus === 'failed' || executionStatus === 'canceled';
  if (failed) {
    // Matching names are evidence, not a success conclusion. Keep the names, drop
    // the success semantics and the separate failure banner this row replaces.
    if (modelAudit.equalModelIds.length === 0) {
      return { tone: 'danger', message: t('requests.tooltips.upstreamModelRequestFailed') };
    }
    return {
      tone: 'muted',
      message: t('requests.detail.upstreamModelMatchedButFailed', { model: modelAudit.equalModelIds.join(', ') }),
    };
  }

  if (modelAudit.status === 'matched') {
    return { tone: 'success', message: t('requests.detail.upstreamModelMatched') };
  }

  return { tone: 'muted', message: t('requests.tooltips.upstreamModelUnknown') };
}
