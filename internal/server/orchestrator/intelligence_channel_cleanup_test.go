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

func createIntelligenceCleanupChannel(t *testing.T, ctx context.Context, client *ent.Client, name string) *ent.Channel {
	t.Helper()

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName(name).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: name}).
		SetSupportedModels([]string{"m"}).
		SetDefaultTestModel("m").
		Save(ctx)
	require.NoError(t, err)

	return ch
}

func newIntelligenceDeletionRegressionServices(t *testing.T, dsn string) (*biz.ChannelService, *IntelligenceService, *ent.Client, context.Context) {
	t.Helper()

	service, client, ctx := newIntelligenceCleanupService(t, dsn)
	channels := biz.NewChannelServiceForTest(client)
	channels.SetChannelDeletionHook(service)

	return channels, service, client, ctx
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

	alive := createIntelligenceCleanupChannel(t, ctx, client, "Alive")

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

// Deleting one channel must remove its intelligence target and history while
// leaving the other configured channels untouched.
func TestDeleteChannelCleansIntelligenceConfigAndHistory(t *testing.T) {
	channels, service, client, ctx := newIntelligenceDeletionRegressionServices(t, "file:intel-delete-single?mode=memory&_fk=1")

	keep := createIntelligenceCleanupChannel(t, ctx, client, "Keep")
	remove := createIntelligenceCleanupChannel(t, ctx, client, "Remove")

	storeIntelligenceCleanupConfig(t, ctx, service,
		objects.IntelligenceTarget{ChannelID: keep.ID, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: remove.ID, ModelID: "m"},
	)
	createIntelligenceCleanupRun(t, ctx, client, keep.ID)
	createIntelligenceCleanupRun(t, ctx, client, remove.ID)

	require.NoError(t, channels.DeleteChannel(ctx, remove.ID))

	config, err := intelligenceConfigFromClient(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Targets, 1)
	require.Equal(t, keep.ID, config.Targets[0].ChannelID)

	removedRuns, err := client.IntelligenceRun.Query().Where(intelligencerun.ChannelID(remove.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, removedRuns, "the deleted channel's history is no longer visible")

	keptRuns, err := client.IntelligenceRun.Query().Where(intelligencerun.ChannelID(keep.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, keptRuns, "the surviving channel keeps its history")
}

// Bulk deletion must clean every deleted channel in the same transaction and
// leave the targets and history of the surviving channels intact.
func TestBulkDeleteChannelsCleansIntelligenceConfigAndHistory(t *testing.T) {
	channels, service, client, ctx := newIntelligenceDeletionRegressionServices(t, "file:intel-delete-bulk?mode=memory&_fk=1")

	keepA := createIntelligenceCleanupChannel(t, ctx, client, "Keep A")
	keepB := createIntelligenceCleanupChannel(t, ctx, client, "Keep B")
	removeA := createIntelligenceCleanupChannel(t, ctx, client, "Remove A")
	removeB := createIntelligenceCleanupChannel(t, ctx, client, "Remove B")

	storeIntelligenceCleanupConfig(t, ctx, service,
		objects.IntelligenceTarget{ChannelID: keepA.ID, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: keepB.ID, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: removeA.ID, ModelID: "m"},
		objects.IntelligenceTarget{ChannelID: removeB.ID, ModelID: "m"},
	)
	for _, id := range []int{keepA.ID, keepB.ID, removeA.ID, removeB.ID} {
		createIntelligenceCleanupRun(t, ctx, client, id)
	}

	require.NoError(t, channels.BulkDeleteChannels(ctx, []int{removeA.ID, removeB.ID}))

	config, err := intelligenceConfigFromClient(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Targets, 2)
	require.ElementsMatch(t, []int{keepA.ID, keepB.ID}, []int{
		config.Targets[0].ChannelID,
		config.Targets[1].ChannelID,
	})

	removedRuns, err := client.IntelligenceRun.Query().
		Where(intelligencerun.ChannelIDIn(removeA.ID, removeB.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, removedRuns, "both deleted channels' history is no longer visible")

	keptRuns, err := client.IntelligenceRun.Query().
		Where(intelligencerun.ChannelIDIn(keepA.ID, keepB.ID)).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, keptRuns, "surviving channels keep their history")
}
