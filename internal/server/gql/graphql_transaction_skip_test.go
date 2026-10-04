package gql

import (
	"testing"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

func TestSkipGraphQLTransaction(t *testing.T) {
	tests := []struct {
		name  string
		query string
		skip  bool
	}{
		{
			name:  "intelligence check runs without the outer transaction",
			query: `mutation EvaluateChannelIntelligence($input: IntelligenceEvaluateInput!) { evaluateChannelIntelligence(input: $input) { total } }`,
			skip:  true,
		},
		{
			name:  "intelligence check skips under any operation name",
			query: `mutation CustomName { evaluateChannelIntelligence(input: {channelID: "1"}) { total } }`,
			skip:  true,
		},
		{
			name:  "anonymous intelligence check skips",
			query: `mutation { evaluateChannelIntelligence(input: {channelID: "1"}) { total } }`,
			skip:  true,
		},
		{
			name:  "test channel still skips",
			query: `mutation TestChannel($input: TestChannelInput!) { testChannel(input: $input) { success } }`,
			skip:  true,
		},
		{
			name:  "per-row bulk import still skips",
			query: `mutation BulkImportChannels($input: BulkImportChannelsInput!) { bulkImportChannels(input: $input) { success } }`,
			skip:  true,
		},
		{
			name:  "ordinary mutation keeps the transaction",
			query: `mutation CreateChannel($input: CreateChannelInput!) { createChannel(input: $input) { id } }`,
			skip:  false,
		},
		{
			name:  "queries keep the transaction",
			query: `query Channels { channels { edges { node { id } } } }`,
			skip:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parser.ParseQuery(&ast.Source{Input: tt.query})
			if err != nil {
				t.Fatalf("parsing query: %v", err)
			}

			if got := skipGraphQLTransaction(doc.Operations[0]); got != tt.skip {
				t.Fatalf("skipGraphQLTransaction() = %v, want %v", got, tt.skip)
			}
		})
	}
}
