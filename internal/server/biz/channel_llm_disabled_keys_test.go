package biz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
)

// The intelligence check disables the key it found degraded, which leaves the
// channel with no enabled key at all. Re-checking that key is the only way to
// find out whether it recovered, so an explicit key has to be enough to build
// the channel — otherwise the check can never see the recovery it is waiting for.
func TestBuildChannelWithKeyWhenEveryStoredKeyIsDisabled(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	const key = "sk-disabled-key"

	entChannel := client.Channel.Create().
		SetName("Disabled Key Channel").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{key}}).
		SetDisabledAPIKeys([]objects.DisabledAPIKey{{
			Key:    key,
			Reason: "intelligence check: degraded",
		}}).
		SetSupportedModels([]string{"gpt-4o"}).
		SetDefaultTestModel("gpt-4o").
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)

	// Without a key to force, the channel cannot be built: there is nothing left
	// to authenticate with.
	_, err := channelSvc.buildChannelWithTransformer(entChannel)
	require.Error(t, err, "a channel with no enabled key must not build on its own")

	built, err := channelSvc.buildChannelWithTransformer(entChannel, key)
	require.NoError(t, err, "the disabled key is still the key under test")
	require.NotNil(t, built)
	require.NotNil(t, built.Outbound)
}

// The key test flow reads a channel to probe one key, and it uses the same
// helper. It has to work on a channel whose stored keys are all disabled for the
// same reason.
func TestGetChannelWithKeyIgnoresDisabledState(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	const key = "sk-disabled-key"

	entChannel := client.Channel.Create().
		SetName("Disabled Key Channel").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKeys: []string{key}}).
		SetDisabledAPIKeys([]objects.DisabledAPIKey{{
			Key:    key,
			Reason: "intelligence check: degraded",
		}}).
		SetSupportedModels([]string{"gpt-4o"}).
		SetDefaultTestModel("gpt-4o").
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)

	built, err := channelSvc.GetChannelWithKey(ctx, entChannel.ID, key)
	require.NoError(t, err)
	require.NotNil(t, built)
	require.Equal(t, entChannel.ID, built.ID)
}
