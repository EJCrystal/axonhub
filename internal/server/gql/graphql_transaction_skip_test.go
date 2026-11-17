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
				t.Fatalf("skipGraphQLTransaction() = %v, want %v", got, tt.skip))

func mutationOp(name string, fields ...string) *ast.OperationDefinition {
	op := &ast.OperationDefinition{
		Operation: "mutation",
		Name:      name,
	}
	for _, f := range fields {
		op.SelectionSet = append(op.SelectionSet, &ast.Field{Name: f})
	}
	return op
}

func TestSkipGraphqlTransaction(t *testing.T) {
	cases := []struct {
		name     string
		op       *ast.OperationDefinition
		wantSkip bool
	}{
		{
			name:     "checkProviderQuotas skips under any client-chosen operation name",
			op:       mutationOp("AnythingIWant", "checkProviderQuotas"),
			wantSkip: true,
		},
		{
			name:     "checkProviderQuotas with the canonical name",
			op:       mutationOp("CheckProviderQuotas", "checkProviderQuotas"),
			wantSkip: true,
		},
		{
			name:     "resetChannelQuotaNow skips",
			op:       mutationOp("Whatever", "resetChannelQuotaNow"),
			wantSkip: true,
		},
		{
			name:     "quota mutation nested among other fields",
			op:       mutationOp("Mixed", "disableChannelAPIKey", "checkProviderQuotas"),
			wantSkip: true,
		},
		{
			name:     "ordinary channel mutation does not skip",
			op:       mutationOp("DisableChannelAPIKey", "disableChannelAPIKey"),
			wantSkip: false,
		},
		{
			name:     "TestChannel operation skips via name match",
			op:       mutationOp("TestChannel", "testChannel"),
			wantSkip: true,
		},
		{
			name:     "bulkImportChannels skips",
			op:       mutationOp("Import", "bulkImportChannels"),
			wantSkip: true,
		},
		{
			name:     "empty selection set does not skip",
			op:       mutationOp("Empty"),
			wantSkip: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skipGraphqlTransaction(tc.op); got != tc.wantSkip {
				t.Fatalf("skipGraphqlTransaction(%s) = %v, want %v", tc.op.Name, got, tc.wantSkip)
			}
		})
	}
}
