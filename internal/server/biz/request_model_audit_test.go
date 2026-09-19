package biz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
)

func TestAuditRequestModels(t *testing.T) {
	tests := []struct {
		name                              string
		executions                        []*ent.RequestExecution
		status                            objects.ModelAuditStatus
		compared, unknown, conflicts      int
		upstream, mismatched, conflicting []string
	}{
		{name: "no executions", status: objects.ModelAuditUnknown},
		{name: "routing name cannot substitute for final sent name", executions: []*ent.RequestExecution{
			{ModelID: "routed", UpstreamModelID: "routed"},
		}, status: objects.ModelAuditUnknown, unknown: 1, upstream: []string{"routed"}},
		{name: "override matches actual sent name", executions: []*ent.RequestExecution{
			{ModelID: "routed", OutboundModelID: "sent", UpstreamModelID: "sent"},
		}, status: objects.ModelAuditMatched, compared: 1, upstream: []string{"sent"}},
		{name: "override reveals false match", executions: []*ent.RequestExecution{
			{ModelID: "routed", OutboundModelID: "sent", UpstreamModelID: "routed"},
		}, status: objects.ModelAuditMismatched, compared: 1, upstream: []string{"routed"}, mismatched: []string{"routed"}},
		{name: "retry cross matching and partial unknown", executions: []*ent.RequestExecution{
			{OutboundModelID: "a", UpstreamModelID: "a"},
			{OutboundModelID: "b", UpstreamModelID: "a"},
			{OutboundModelID: "c"},
		}, status: objects.ModelAuditMismatched, compared: 2, unknown: 1, upstream: []string{"a"}, mismatched: []string{"a"}},
		{name: "partial metadata cannot establish all matched", executions: []*ent.RequestExecution{
			{OutboundModelID: "a", UpstreamModelID: "a"},
			{OutboundModelID: "a", UpstreamModelID: "   "},
		}, status: objects.ModelAuditUnknown, compared: 1, unknown: 1, upstream: []string{"a"}},
		{name: "exact names retain whitespace case and version", executions: []*ent.RequestExecution{
			{OutboundModelID: "a", UpstreamModelID: "A"},
			{OutboundModelID: "a", UpstreamModelID: " a "},
			{OutboundModelID: "a", UpstreamModelID: "a-2026-09-19"},
		}, status: objects.ModelAuditMismatched, compared: 3, upstream: []string{"A", " a ", "a-2026-09-19"}, mismatched: []string{"A", " a ", "a-2026-09-19"}},
		{name: "stream changes preserve conflict and other mismatches", executions: []*ent.RequestExecution{
			{OutboundModelID: "a", UpstreamModelID: "a", UpstreamModelIds: []string{"a", "b"}},
			{OutboundModelID: "c", UpstreamModelID: "d"},
			{ModelID: "historical"},
		}, status: objects.ModelAuditConflicting, compared: 2, unknown: 1, conflicts: 1,
			upstream: []string{"a", "b", "d"}, mismatched: []string{"b", "d"}, conflicting: []string{"a", "b"}},
		{name: "stream conflict is known even when sent name is missing", executions: []*ent.RequestExecution{
			{UpstreamModelIds: []string{"a", "b"}},
		}, status: objects.ModelAuditConflicting, unknown: 1, conflicts: 1, upstream: []string{"a", "b"}, conflicting: []string{"a", "b"}},
		{name: "different retries and repeated metadata are not stream conflicts", executions: []*ent.RequestExecution{
			{OutboundModelID: "a", UpstreamModelID: "a", UpstreamModelIds: []string{"a", "a"}},
			{OutboundModelID: "b", UpstreamModelIds: []string{"b"}},
		}, status: objects.ModelAuditMatched, compared: 2, upstream: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := AuditRequestModels(tt.executions)
			require.Equal(t, tt.status, audit.Status)
			require.Equal(t, tt.compared, audit.ComparedCount)
			require.Equal(t, tt.unknown, audit.UnknownCount)
			require.Equal(t, tt.conflicts, audit.ConflictCount)
			require.ElementsMatch(t, tt.upstream, audit.UpstreamModelIds)
			require.ElementsMatch(t, tt.mismatched, audit.MismatchedModelIds)
			require.ElementsMatch(t, tt.conflicting, audit.ConflictingModelIds)
			require.NotNil(t, audit.UpstreamModelIds)
			require.NotNil(t, audit.MismatchedModelIds)
			require.NotNil(t, audit.ConflictingModelIds)
		})
	}
}
