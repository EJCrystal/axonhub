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
  trigger: z.string().default('manual'),
  status: z.enum(['succeeded', 'failed', 'partial']),
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
});
export type IntelligenceTarget = z.infer<typeof intelligenceTargetSchema>;

export const intelligenceConfigSchema = z.object({
  enabled: z.boolean().default(false),
  intervalMinutes: z.number().default(60),
  targets: z.array(intelligenceTargetSchema).default([]),
});
export type IntelligenceConfig = z.infer<typeof intelligenceConfigSchema>;

// The intervals the backend accepts.
export const INTELLIGENCE_INTERVALS = [10, 30, 60] as const;
export type IntelligenceInterval = (typeof INTELLIGENCE_INTERVALS)[number];
