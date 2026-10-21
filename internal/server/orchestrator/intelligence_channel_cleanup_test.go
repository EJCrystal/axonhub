package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/intelligencerun"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func newIntelligenceCleanupService(t *testing.T, dsn string) (*IntelligenceService, *ent.Client, context.Context) {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	systemService := biz.NewSystemService(biz.SystemServiceParams{
		Ent:         client,
		CacheConfig: xcache.Config{Mode: xcache.ModeMemory},
	})

	return &IntelligenceService{ent: client, systemService: systemService}, client, ctx
}

func storeIntelligenceCleanupConfig(t *testing.T, ctx context.Context, service *IntelligenceService, targets ...objects.IntelligenceTarget) {
	t.Helper()

	value, err := json.Marshal(&objects.IntelligenceConfig{
		Enabled:        true,
		CronExpression: "0 * * * *",
		Targets:        targets,
	})
	require.NoError(t, err)
	require.NoError(t, service.systemService.SetSystemValue(ctx, IntelligenceConfigKey, string(value)))
}

func createIntelligenceCleanupRun(t *testing.T, ctx context.Context, client *ent.Client, channelID int) {
	t.Helper()

	_, err := client.IntelligenceRun.Create().
		SetChannelID(channelID).
		SetModelID("m").
		Save(ctx)
	require.NoError(t, err)
}

func TestIntelligenceService_OnChannelsDeletedRemovesTargetsAndRuns(t *testing.T) {
	service, client, ctx := newIntelligenceCleanupService(t, "file:intel-cleanup-hook?mode=memory&_fk=1")

	storeIntelligenceCleanupConfig(t, ctx, service,
		objects.IntelligenceTarget{ChannelID: 1, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: 2, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: 3, ModelID: "m"},
	)
	for _, id := range []int{1, 2, 3} {
		createIntelligenceCleanupRun(t, ctx, client, id)
	}

	require.NoError(t, service.OnChannelsDeleted(ctx, []int{2, 3}))

	config, err := intelligenceConfigFromClient(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Targets, 1)
	require.Equal(t, 1, config.Targets[0].ChannelID)

	remaining, err := client.IntelligenceRun.Query().Where(intelligencerun.ChannelIDIn(2, 3)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, remaining, "history of the deleted channels is no longer visible")

	kept, err := client.IntelligenceRun.Query().Where(intelligencerun.ChannelID(1)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, kept)
}

func TestIntelligenceService_PruneDeletedChannelStateCleansLegacyRows(t *testing.T) {
	service, client, ctx := newIntelligenceCleanupService(t, "file:intel-cleanup-prune?mode=memory&_fk=1")

	alive, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Alive").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "alive"}).
		SetSupportedModels([]string{"m"}).
		SetDefaultTestModel("m").
		Save(ctx)
	require.NoError(t, err)

	storeIntelligenceCleanupConfig(t, ctx, service,
		objects.IntelligenceTarget{ChannelID: alive.ID, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: 56, ModelID: "m"},
	)
	createIntelligenceCleanupRun(t, ctx, client, alive.ID)
	createIntelligenceCleanupRun(t, ctx, client, 56)

	require.NoError(t, service.pruneDeletedChannelState(ctx))

	config, err := intelligenceConfigFromClient(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Targets, 1)
	require.Equal(t, alive.ID, config.Targets[0].ChannelID)

	missing, err := client.IntelligenceRun.Query().Where(intelligencerun.ChannelID(56)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, missing, "legacy history of a deleted channel is removed")

	kept, err := client.IntelligenceRun.Query().Where(intelligencerun.ChannelID(alive.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, kept)
}
