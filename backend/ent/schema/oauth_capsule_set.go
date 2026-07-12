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

// OAuthCapsuleSet stores an immutable copy-on-write capsule version for a pool.
type OAuthCapsuleSet struct {
	ent.Schema
}

func (OAuthCapsuleSet) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_capsule_sets"}}
}

func (OAuthCapsuleSet) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (OAuthCapsuleSet) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("pool_id"),
		field.Int64("version"),
		field.String("compatibility_digest").MaxLen(128).NotEmpty(),
		field.JSON("payload", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (OAuthCapsuleSet) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("pool", OAuthPool.Type).
			Ref("capsule_sets").
			Field("pool_id").
			Unique().
			Required(),
	}
}

func (OAuthCapsuleSet) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("pool_id", "version").Unique(),
		index.Fields("compatibility_digest"),
	}
}
