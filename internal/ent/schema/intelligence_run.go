package schema

import (
	"entgo.io/contrib/entgql"
	"entgo.io/ent"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/scopes"
)

// IntelligenceRun records one intelligence check of one channel. A run covers
// every enabled API key of the channel, so a single row carries the per-key
// detail in its results payload.
type IntelligenceRun struct {
	ent.Schema
}

func (IntelligenceRun) Mixin() []ent.Mixin {
	return []ent.Mixin{
		TimeMixin{},
		schematype.SoftDeleteMixin{},
	}
}

func (IntelligenceRun) Indexes() []ent.Index {
	return []ent.Index{
		// History is read per channel, newest first, and trimmed to the most
		// recent runs of that channel.
		index.Fields("channel_id", "created_at").
			StorageKey("intelligence_runs_by_channel_id_created_at"),
	}
}

func (IntelligenceRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int("channel_id").
			Immutable().
			Comment("Channel this run evaluated"),
		field.String("channel_name").
			Default("").
			Comment("Channel name captured at run time, so history survives a rename"),
		field.String("model_id").
			Comment("Model the channel was asked to generate with"),
		field.String("trigger").
			Default("manual").
			Comment("What started the run: manual or scheduled"),
		field.Enum("status").
			Values("succeeded", "failed", "partial").
			Default("failed").
			Comment("succeeded when every key passed, failed when none did, partial in between"),
		field.Int("total_keys").
			Default(0).
			Comment("Number of API keys evaluated"),
		field.Int("success_keys").
			Default(0).
			Comment("Number of keys whose assessment succeeded"),
		field.Int("failed_keys").
			Default(0).
			Comment("Number of keys whose assessment failed"),
		field.Int("duration_ms").
			Default(0).
			Comment("Wall-clock duration of the whole run"),
		field.JSON("results", []objects.IntelligenceKeyResult{}).
			Optional().
			Comment("Per-key outcomes, including the generated source"),
	}
}

func (IntelligenceRun) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entgql.QueryField(),
		entgql.RelayConnection(),
	}
}

func (IntelligenceRun) Policy() ent.Policy {
	return scopes.Policy{
		Query: scopes.QueryPolicy{
			scopes.UserReadScopeRule(scopes.ScopeReadChannels),
			scopes.OwnerRule(),
		},
		Mutation: scopes.MutationPolicy{
			scopes.UserWriteScopeRule(scopes.ScopeWriteChannels),
			scopes.OwnerRule(),
		},
	}
}
