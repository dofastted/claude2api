package schema

import (
	"github.com/dofastted/claude2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthPool defines a strict Anthropic OAuth credential and egress boundary.
type OAuthPool struct {
	ent.Schema
}

func (OAuthPool) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_pools"}}
}

func (OAuthPool) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (OAuthPool) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.String("provider").MaxLen(32).Default("claude_oauth"),
		field.String("status").MaxLen(20).Default("active"),
		field.String("mode").MaxLen(20).Default("shadow"),
		field.Int64("egress_route_id"),
		field.JSON("allowed_origins", []string{}).
			Default([]string{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("allowed_models", []string{}).
			Default([]string{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int64("active_capsule_set_version").Default(0),
		field.Int64("previous_capsule_set_version").Optional().Nillable(),
		field.String("compatibility_digest").MaxLen(128).Default(""),
		field.Int("session_ttl_seconds").Default(3600),
		field.Time("shadow_started_at").Optional().Nillable(),
		field.Time("shadow_qualified_at").Optional().Nillable(),
	}
}

func (OAuthPool) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("egress_route", Proxy.Type).
			Ref("oauth_pools").
			Field("egress_route_id").
			Unique().
			Required(),
		edge.To("credentials", OAuthPoolCredential.Type),
		edge.To("capsule_sets", OAuthCapsuleSet.Type),
		edge.To("groups", Group.Type),
	}
}

func (OAuthPool) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("mode"),
		index.Fields("egress_route_id"),
		index.Fields("deleted_at"),
	}
}
