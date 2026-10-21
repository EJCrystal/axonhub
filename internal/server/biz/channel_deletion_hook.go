package biz

import (
	"context"

	"github.com/looplj/axonhub/internal/log"
)

// ChannelDeletionHook lets the channel service remove per-channel state owned
// by another service in the same transaction as the channel deletion.
//
// The interface lives in biz to avoid an import cycle with orchestrator.
type ChannelDeletionHook interface {
	OnChannelsDeleted(ctx context.Context, channelIDs []int) error
}

// SetChannelDeletionHook wires the optional deletion hook. Called once at
// start-up from the orchestrator fx module.
func (svc *ChannelService) SetChannelDeletionHook(hook ChannelDeletionHook) {
	svc.deletionHook = hook
}

// notifyChannelsDeleted runs the deletion hook, if one is registered. The hook
// receives the transaction context so its writes commit or roll back with the
// channel deletion.
func (svc *ChannelService) notifyChannelsDeleted(ctx context.Context, channelIDs []int) error {
	if svc.deletionHook == nil || len(channelIDs) == 0 {
		return nil
	}

	if err := svc.deletionHook.OnChannelsDeleted(ctx, channelIDs); err != nil {
		return err
	}

	log.Info(ctx, "channel deletion hook completed", log.Int("count", len(channelIDs)))
	return nil
}
