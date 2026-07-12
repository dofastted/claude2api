package schema

import (
	"github.com/dofastted/claude2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthPoolCredential binds one Anthropic OAuth account to exactly one pool.
type OAuthPoolCredential struct {
	ent.Schema
}

func (OAuthPoolCredential) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_pool_credentials"}}
}

func (OAuthPoolCredential) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OAuthPoolCredential) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("pool_id"),
		field.Int64("account_id").Unique(),
		field.String("state").MaxLen(20).Default("available"),
		field.Time("cooldown_until").Optional().Nillable(),
	}
}

func (OAuthPoolCredential) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("pool", OAuthPool.Type).
			Ref("credentials").
			Field("pool_id").
			Unique().
			Required(),
		edge.From("account", Account.Type).
			Ref("oauth_pool_credential").
			Field("account_id").
			Unique().
			Required(),
	}
}

func (OAuthPoolCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("pool_id"),
		index.Fields("state"),
		index.Fields("pool_id", "state"),
	}
}
