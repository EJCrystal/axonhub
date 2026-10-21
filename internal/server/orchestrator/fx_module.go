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
	fx.Invoke(func(lc fx.Lifecycle, svc *IntelligenceService, s *scheduler.Scheduler, channels *biz.ChannelService) {
		// A key replaced on a channel must not leave an intelligence target
		// pinning a credential that no longer exists.
		channels.SetChannelAPIKeyRotator(svc)
		// A deleted channel must not leave its intelligence targets and history
		// pointing at an ID the admin console can no longer resolve.
		channels.SetChannelDeletionHook(svc)

		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				return svc.RegisterScheduledTasks(ctx, s)
			},
			OnStop: func(context.Context) error {
				// The configuration watcher polls for the life of the process; stop it
				// with the scheduler rather than leaving it to tick against a closed client.
				svc.stopConfigWatcher()

				return nil
			},
		})
	}),
)
