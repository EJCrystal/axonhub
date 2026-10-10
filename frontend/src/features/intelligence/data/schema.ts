import { z } from 'zod';

// A key outcome recorded inside one run. Mirrors objects.IntelligenceKeyResult.
export const intelligenceKeyResultSchema = z.object({
  keyPrefix: z.string().default(''),
  success: z.boolean().default(false),
  quality: z.string().default(''),
  label: z.string().default(''),
  reason: z.string().default(''),
  taskID: z.string().default(''),
  generationMs: z.number().default(0),
  durationMs: z.number().default(0),
  // Null whenever the run failed before generation or the source was over
  // the size cap, so normalize it the way the rest of the payload treats
  // absent strings. A bare default('') only covers undefined, and a null
  // from GraphQL would make the whole history parse fail.
  html: z.string().nullish().transform((value) => value ?? ''),
  // The candy question's answer, kept apart from html so the history renders
  // prose as prose instead of trying to load it as a page.
  answer: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  error: z.string().optional().nullable(),
  // An operator's verdict, set when automatic scoring could not decide. The
  // backend sends null when there is none, and default() only covers
  // undefined, so a bare default would fail the whole history parse.
  manualVerdict: z.string().nullish().transform((value) => value ?? ''),
});
export type IntelligenceKeyResult = z.infer<typeof intelligenceKeyResultSchema>;

export const intelligenceRunSchema = z.object({
  id: z.string(),
  createdAt: z.string(),
  channelID: z.number(),
  channelName: z.string().default(''),
  modelID: z.string().default(''),
  // Thinking level the run requested. Empty on runs recorded before the field
  // existed, and when the provider default applied.
  reasoningEffort: z.string().default(''),
  // Which check the run used. Empty on runs recorded before the field existed,
  // which were all pelican.
  benchmark: z.string().nullish().transform((value) => value ?? ''),
  trigger: z.string().default('manual'),
  // running while the check is in flight; the rest are terminal.
  status: z.enum(['running', 'succeeded', 'failed', 'partial']),
  totalKeys: z.number().default(0),
  successKeys: z.number().default(0),
  failedKeys: z.number().default(0),
  durationMs: z.number().default(0),
  results: z.array(intelligenceKeyResultSchema).default([]),
});
export type IntelligenceRun = z.infer<typeof intelligenceRunSchema>;

export const intelligenceRunConnectionSchema = z.object({
  edges: z
    .array(z.object({ node: intelligenceRunSchema, cursor: z.string() }))
    .default([]),
  totalCount: z.number().default(0),
  pageInfo: z
    .object({
      hasNextPage: z.boolean().default(false),
      hasPreviousPage: z.boolean().default(false),
      startCursor: z.string().optional().nullable(),
      endCursor: z.string().optional().nullable(),
    })
    .default({ hasNextPage: false, hasPreviousPage: false }),
});
export type IntelligenceRunConnection = z.infer<typeof intelligenceRunConnectionSchema>;

export const intelligenceTargetSchema = z.object({
  channelID: z.string(),
  channelName: z.string().default(''),
  modelID: z.string().default(''),
  // The single key this target evaluates. Null when every enabled key runs.
  apiKey: z.string().optional().nullable(),
  // Thinking level requested for this target. Null means the provider default;
  // a bare default('') only covers undefined, and GraphQL sends null here.
  reasoningEffort: z.string().nullish().transform((value) => value ?? ''),
  // Per-run budget in minutes. Null means the built-in default applies, and
  // GraphQL sends null for that, so normalize it the same way.
  timeoutMinutes: z.number().nullish().transform((value) => value ?? 0),
  // Which check this target runs. Null means the pelican default; GraphQL
  // sends null for that, so normalize it the same way.
  benchmark: z.string().nullish().transform((value) => value ?? ''),
});
export type IntelligenceTarget = z.infer<typeof intelligenceTargetSchema>;

export const intelligenceConfigSchema = z.object({
  enabled: z.boolean().default(false),
  intervalMinutes: z.number().default(60),
  // Off unless switched on: disabling a key also takes it out of the
  // channel's live rotation, so it is opt-in.
  disableDegradedKeys: z.boolean().default(false),
  targets: z.array(intelligenceTargetSchema).default([]),
});
export type IntelligenceConfig = z.infer<typeof intelligenceConfigSchema>;

// The intervals the backend accepts.
export const INTELLIGENCE_INTERVALS = [10, 30, 60] as const;
export type IntelligenceInterval = (typeof INTELLIGENCE_INTERVALS)[number];
