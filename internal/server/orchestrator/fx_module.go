package orchestrator

import (
	"context"

	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/scheduler"
)

var Module = fx.Module("orchestrator",
	fx.Provide(NewDefaultSelector),
	fx.Provide(NewCandidateSelectorDiagnostics),
	fx.Provide(NewChannelLimiterManager),
	fx.Provide(func(svc *biz.ProviderQuotaService) ProviderQuotaStatusProvider { return svc }),
	fx.Provide(NewTestChannelOrchestrator),
	fx.Provide(NewIntelligenceService),
	fx.Invoke(func(lc fx.Lifecycle, svc *IntelligenceService, s *scheduler.Scheduler) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				return svc.RegisterScheduledTasks(ctx, s)
			},
		})
	}),
)
