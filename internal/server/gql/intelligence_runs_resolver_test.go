package gql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"entgo.io/contrib/entgql"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"

	"github.com/looplj/axonhub/internal/ent/intelligencerun"
)

// TestIntelligenceRunsSpansChannelsNewestFirst covers the unfiltered history
// view: it merges every channel's runs and returns them newest first, which is
// what the page relies on to group them per channel.
func TestIntelligenceRunsSpansChannelsNewestFirst(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:intel-runs?mode=memory&_fk=1")
	defer client.Close()

	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))

	for _, run := range []struct {
		channelID   int
		channelName string
		endMs       int64
	}{
		{channelID: 1, channelName: "first", endMs: 1000},
		{channelID: 2, channelName: "second", endMs: 2000},
		{channelID: 1, channelName: "first", endMs: 3000},
	} {
		_, err := client.IntelligenceRun.Create().
			SetChannelID(run.channelID).
			SetChannelName(run.channelName).
			SetModelID("gpt-6.1-sol").
			SetTrigger("manual").
			SetStatus(intelligencerun.StatusSucceeded).
			SetDurationMs(int(run.endMs)).
			Save(ctx)
		require.NoError(t, err)
	}

	resolver := &queryResolver{&Resolver{client: client}}
	orderBy := &ent.IntelligenceRunOrder{
		Direction: entgql.OrderDirectionDesc,
		Field:     ent.IntelligenceRunOrderFieldCreatedAt,
	}

	connection, err := resolver.IntelligenceRuns(ctx, nil, intPtr(50), nil, nil, orderBy, nil)
	require.NoError(t, err)
	require.Len(t, connection.Edges, 3)

	got := make([]int, 0, len(connection.Edges))
	for _, edge := range connection.Edges {
		got = append(got, edge.Node.DurationMs)
	}
	require.Equal(t, []int{3000, 2000, 1000}, got, "runs must come back newest first across channels")

	_, err = resolver.IntelligenceRuns(ctx, nil, intPtr(0), nil, nil, nil, nil)
	require.Error(t, err, "first=0 must be rejected like the other connections")
}

func intPtr(v int) *int { return &v }
