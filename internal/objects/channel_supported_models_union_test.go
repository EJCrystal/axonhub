package objects

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// threeKeyCredentials builds a channel with three keys that each serve an
// explicit, overlapping per-key model list:
//
//	k1 -> m1, m2
//	k2 -> m2, m3
//	k3 -> m4, m5
//
// so the channel catalog (union of every key) is m1..m5.
func threeKeyCredentials() *ChannelCredentials {
	return &ChannelCredentials{
		APIKeys: []string{"k1", "k2", "k3"},
		APIKeyModels: []APIKeyModels{
			{APIKey: "k1", Models: []string{"m1", "m2"}},
			{APIKey: "k2", Models: []string{"m2", "m3"}},
			{APIKey: "k3", Models: []string{"m4", "m5"}},
		},
	}
}

// UnionAPIKeyModels is the management-UI catalog: the union across ALL keys,
// regardless of enable/disable state.
func TestUnionAPIKeyModels_PerKeyUnion(t *testing.T) {
	creds := threeKeyCredentials()

	got := creds.UnionAPIKeyModels(nil)

	require.ElementsMatch(t, []string{"m1", "m2", "m3", "m4", "m5"}, got)
}

// Even when every key is disabled the catalog view still lists all per-key
// models, while the servable set (UnionEnabledAPIKeyModels) collapses to empty
// so routing and /v1/models hide a channel that can serve nothing right now.
func TestUnionAPIKeyModels_AllKeysDisabled_DisplaysFullCatalog(t *testing.T) {
	creds := threeKeyCredentials()
	disabled := []DisabledAPIKey{{Key: "k1"}, {Key: "k2"}, {Key: "k3"}}

	catalog := creds.UnionAPIKeyModels(nil)
	servable := creds.UnionEnabledAPIKeyModels(nil, disabled)

	require.ElementsMatch(t, []string{"m1", "m2", "m3", "m4", "m5"}, catalog,
		"catalog must survive all keys being disabled")
	require.Empty(t, servable,
		"nothing is servable once every key is disabled")
}

// Disabling one key drops only the models unique to it from the servable set;
// the catalog is unaffected.
func TestUnionEnabledAPIKeyModels_PartialDisable_HidesOnlyOrphanedModels(t *testing.T) {
	creds := threeKeyCredentials()
	disabled := []DisabledAPIKey{{Key: "k2"}} // k2 uniquely serves m3

	catalog := creds.UnionAPIKeyModels(nil)
	servable := creds.UnionEnabledAPIKeyModels(nil, disabled)

	require.ElementsMatch(t, []string{"m1", "m2", "m3", "m4", "m5"}, catalog)
	require.ElementsMatch(t, []string{"m1", "m2", "m4", "m5"}, servable,
		"m3 is only served by the disabled key and must drop from servable")
	require.NotContains(t, servable, "m3")
}

// A channel without any per-key assignments must be a no-op: the catalog view
// returns the stored channel list unchanged, so Option A never alters display
// for non per-key channels.
func TestUnionAPIKeyModels_NoPerKeyAssignments_ReturnsStoredList(t *testing.T) {
	creds := &ChannelCredentials{APIKeys: []string{"k1", "k2"}}
	stored := []string{"gpt-5.5", "gpt-5.6-sol"}

	got := creds.UnionAPIKeyModels(stored)

	require.ElementsMatch(t, stored, got)
}
