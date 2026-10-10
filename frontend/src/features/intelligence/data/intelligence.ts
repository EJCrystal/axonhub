import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { graphqlRequest } from '@/gql/graphql';
import { useErrorHandler } from '@/hooks/use-error-handler';
import {
  IntelligenceConfig,
  IntelligenceInterval,
  IntelligenceRunConnection,
  intelligenceConfigSchema,
  intelligenceRunConnectionSchema,
  intelligenceRunSchema,
} from './schema';

const INTELLIGENCE_CONFIG_QUERY = `
  query IntelligenceConfig {
    intelligenceConfig {
      enabled
      intervalMinutes
      disableDegradedKeys
      targets {
        channelID
        channelName
        modelID
        apiKey
        reasoningEffort
        timeoutMinutes
        benchmark
      }
    }
  }
`;

const SET_INTELLIGENCE_CONFIG_MUTATION = `
  mutation SetIntelligenceConfig($input: IntelligenceConfigInput!) {
    setIntelligenceConfig(input: $input) {
      enabled
      intervalMinutes
      disableDegradedKeys
      targets {
        channelID
        channelName
        modelID
        apiKey
        reasoningEffort
        timeoutMinutes
        benchmark
      }
    }
  }
`;

const RUN_INTELLIGENCE_CHECK_NOW_MUTATION = `
  mutation RunIntelligenceCheckNow($channelID: ID, $apiKey: String) {
    runIntelligenceCheckNow(channelID: $channelID, apiKey: $apiKey)
  }
`;

const INTELLIGENCE_HISTORY_QUERY = `
  query IntelligenceHistory($channelID: ID!, $first: Int) {
    intelligenceHistory(channelID: $channelID, first: $first) {
      totalCount
      pageInfo {
        hasNextPage
        hasPreviousPage
        startCursor
        endCursor
      }
      edges {
        cursor
        node {
          id
          createdAt
          channelID
          channelName
          modelID
          reasoningEffort
          benchmark
          trigger
          status
          totalKeys
          successKeys
          failedKeys
          durationMs
          results {
            keyPrefix
            success
            quality
            label
            reason
            taskID
            generationMs
            durationMs
            html
            answer
            error
            manualVerdict
          }
        }
      }
    }
  }
`;

const ALL_INTELLIGENCE_RUNS_QUERY = `
  query AllIntelligenceRuns($first: Int) {
    intelligenceRuns(first: $first, orderBy: { field: CREATED_AT, direction: DESC }) {
      totalCount
      edges {
        cursor
        node {
          id
          createdAt
          channelID
          channelName
          modelID
          reasoningEffort
          benchmark
          trigger
          status
          totalKeys
          successKeys
          failedKeys
          durationMs
          results {
            keyPrefix
            success
            quality
            label
            reason
            taskID
            generationMs
            durationMs
            html
            answer
            error
            manualVerdict
          }
        }
      }
    }
  }
`;

export function useIntelligenceConfig() {
  return useQuery({
    queryKey: ['intelligence-config'],
    queryFn: async () => {
      const data = await graphqlRequest<{ intelligenceConfig: unknown }>(INTELLIGENCE_CONFIG_QUERY);
      return intelligenceConfigSchema.parse(data.intelligenceConfig);
    },
  });
}

export function useSetIntelligenceConfig() {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async (input: {
      enabled: boolean;
      intervalMinutes: IntelligenceInterval;
      disableDegradedKeys?: boolean;
      targets: {
        channelID: string;
        modelID: string;
        apiKey?: string;
        reasoningEffort?: string;
        timeoutMinutes?: number;
        benchmark?: string;
      }[];
    }) => {
      const data = await graphqlRequest<{ setIntelligenceConfig: unknown }>(SET_INTELLIGENCE_CONFIG_MUTATION, { input });
      return intelligenceConfigSchema.parse(data.setIntelligenceConfig);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['intelligence-config'] });
      toast.success(t('intelligence.messages.saved'));
    },
    onError: (error) => handleError(error),
  });
}

export function useRunIntelligenceCheckNow() {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useMutation({
    // Nothing means every configured target, which is what the button's default
    // action does. A channel narrows the run to it, and a key narrows it to one
    // target, since a channel may be configured once per key.
    mutationFn: async (target?: { channelID?: string; apiKey?: string }) => {
      const data = await graphqlRequest<{ runIntelligenceCheckNow: number }>(RUN_INTELLIGENCE_CHECK_NOW_MUTATION, {
        channelID: target?.channelID ?? null,
        apiKey: target?.apiKey ?? null,
      });
      return data.runIntelligenceCheckNow;
    },
    onSuccess: (queued) => {
      toast.success(t('intelligence.messages.started', { count: queued }));
      // A run takes minutes; refresh the history shortly after so the first
      // finished channel shows up without a manual reload.
      setTimeout(() => {
        queryClient.invalidateQueries({ queryKey: ['intelligence-history'] });
      }, 60_000);
    },
    onError: (error) => handleError(error),
  });
}

export function useIntelligenceHistory(channelID?: string, first = 50) {
  return useQuery({
    queryKey: ['intelligence-history', channelID, first],
    enabled: Boolean(channelID),
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const data = await graphqlRequest<{ intelligenceHistory: unknown }>(INTELLIGENCE_HISTORY_QUERY, {
        channelID,
        first,
      });
      return intelligenceRunConnectionSchema.parse(data.intelligenceHistory) as IntelligenceRunConnection;
    },
  });
}

// useAllIntelligenceRuns powers the unfiltered view: every channel's runs,
// newest first. The backend returns them across channels and the page groups
// them for display.
export function useAllIntelligenceRuns(first = 200) {
  return useQuery({
    queryKey: ['intelligence-runs', 'all', first],
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const data = await graphqlRequest<{ intelligenceRuns: unknown }>(ALL_INTELLIGENCE_RUNS_QUERY, { first });
      return intelligenceRunConnectionSchema.parse(data.intelligenceRuns) as IntelligenceRunConnection;
    },
  });
}

const SET_INTELLIGENCE_RUN_VERDICT_MUTATION = `
  mutation SetIntelligenceRunVerdict($input: SetIntelligenceRunVerdictInput!) {
    setIntelligenceRunVerdict(input: $input) {
      id
      createdAt
      channelID
      channelName
      modelID
      reasoningEffort
      benchmark
      trigger
      status
      totalKeys
      successKeys
      failedKeys
      durationMs
      results {
        keyPrefix
        success
        quality
        label
        reason
        taskID
        generationMs
        durationMs
        html
        answer
        error
        manualVerdict
      }
    }
  }
`;

// useSetIntelligenceRunVerdict records an operator's verdict for one key of a
// run, or clears it when verdict is undefined.
export function useSetIntelligenceRunVerdict() {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const { handleError } = useErrorHandler();

  return useMutation({
    mutationFn: async (input: { runID: string; keyPrefix: string; verdict?: 'normal' | 'degraded' }) => {
      const data = await graphqlRequest<{ setIntelligenceRunVerdict: unknown }>(SET_INTELLIGENCE_RUN_VERDICT_MUTATION, {
        input: { runID: input.runID, keyPrefix: input.keyPrefix, verdict: input.verdict ?? null },
      });
      return intelligenceRunSchema.parse(data.setIntelligenceRunVerdict);
    },
    onSuccess: () => {
      toast.success(t('intelligence.messages.verdictSaved'));
      queryClient.invalidateQueries({ queryKey: ['intelligence-history'] });
      queryClient.invalidateQueries({ queryKey: ['intelligence-runs'] });
    },
    onError: (error) => handleError(error),
  });
}

export type { IntelligenceConfig };
