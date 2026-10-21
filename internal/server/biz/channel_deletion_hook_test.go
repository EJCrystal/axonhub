package biz

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

type recordingChannelDeletionHook struct {
	ids  []int
	inTx bool
	err  error
}

func (h *recordingChannelDeletionHook) OnChannelsDeleted(ctx context.Context, channelIDs []int) error {
	h.ids = append([]int(nil), channelIDs...)
	h.inTx = ent.TxFromContext(ctx) != nil

	return h.err
}

func createDeletionTestChannel(t *testing.T, ctx context.Context, client *ent.Client, name string) *ent.Channel {
	t.Helper()

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName(name).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: name}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	return ch
}

func TestChannelService_DeleteChannelRunsDeletionHookInTransaction(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	ch := createDeletionTestChannel(t, ctx, client, "Delete Hook")

	hook := &recordingChannelDeletionHook{}
	svc.SetChannelDeletionHook(hook)

	require.NoError(t, svc.DeleteChannel(ctx, ch.ID))
	require.Equal(t, []int{ch.ID}, hook.ids)
	require.True(t, hook.inTx, "the hook must share the channel deletion transaction")

	_, err := client.Channel.Get(ctx, ch.ID)
	require.True(t, ent.IsNotFound(err), "the channel is gone after the transaction commits")
}

func TestChannelService_BulkDeleteChannelsRunsDeletionHook(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	ch := createDeletionTestChannel(t, ctx, client, "Bulk Delete Hook")

	hook := &recordingChannelDeletionHook{}
	svc.SetChannelDeletionHook(hook)

	require.NoError(t, svc.BulkDeleteChannels(ctx, []int{ch.ID}))
	require.Equal(t, []int{ch.ID}, hook.ids)
	require.True(t, hook.inTx, "the hook must share the bulk deletion transaction")
}

func TestChannelService_DeleteChannelRollsBackWhenDeletionHookFails(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	ch := createDeletionTestChannel(t, ctx, client, "Delete Hook Failure")

	svc.SetChannelDeletionHook(&recordingChannelDeletionHook{err: errors.New("cleanup failed")})

	err := svc.DeleteChannel(ctx, ch.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cleanup failed")

	_, err = client.Channel.Get(ctx, ch.ID)
	require.NoError(t, err, "a failed cleanup must not leave a deleted channel behind")
}
