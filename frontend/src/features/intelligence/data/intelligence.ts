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
} from './schema';

const INTELLIGENCE_CONFIG_QUERY = `
  query IntelligenceConfig {
    intelligenceConfig {
      enabled
      intervalMinutes
      targets {
        channelID
        channelName
        modelID
        apiKey
      }
    }
  }
`;

const SET_INTELLIGENCE_CONFIG_MUTATION = `
  mutation SetIntelligenceConfig($input: IntelligenceConfigInput!) {
    setIntelligenceConfig(input: $input) {
      enabled
      intervalMinutes
      targets {
        channelID
        channelName
        modelID
        apiKey
      }
    }
  }
`;

const RUN_INTELLIGENCE_CHECK_NOW_MUTATION = `
  mutation RunIntelligenceCheckNow {
    runIntelligenceCheckNow
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
            error
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
      targets: { channelID: string; modelID: string; apiKey?: string }[];
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
    mutationFn: async () => {
      const data = await graphqlRequest<{ runIntelligenceCheckNow: number }>(RUN_INTELLIGENCE_CHECK_NOW_MUTATION);
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

export type { IntelligenceConfig };
