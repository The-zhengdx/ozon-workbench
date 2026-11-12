package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

func jsonb() map[string]string {
	return map[string]string{dialect.Postgres: "jsonb"}
}

// CategoryNode is one branch or leaf of a language-specific category snapshot.
type CategoryNode struct {
	ent.Schema
}

func (CategoryNode) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "category_nodes"}}
}

func (CategoryNode) Fields() []ent.Field {
	return []ent.Field{
		field.String("language").MaxLen(16).NotEmpty(),
		field.Int64("description_category_id"),
		field.Int64("type_id").Default(0),
		field.Int("parent_local_id").Optional().Nillable(),
		field.String("name").MaxLen(255).NotEmpty(),
		field.Bool("disabled").Default(false),
		field.Bool("ancestor_disabled").Default(false),
		field.Bool("is_leaf").Default(false),
		field.Int("level").Default(1),
		field.JSON("path", []string{}),
		field.Int("sort_order").Default(0),
		// Latest attribute contract for this leaf and language. Null on branch nodes.
		field.String("attributes_schema").SchemaType(jsonb()).Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CategoryNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("language", "description_category_id", "type_id").Unique(),
		index.Fields("language", "parent_local_id"),
	}
}

// CategorySnapshot is the metadata for the current tree of one language.
type CategorySnapshot struct {
	ent.Schema
}

func (CategorySnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "category_snapshots"}}
}

func (CategorySnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.String("language").MaxLen(16).NotEmpty().Unique(),
		field.String("source").MaxLen(64).Default(""),
		field.Int("type_count").Default(0),
		field.Int("node_count").Default(0),
		field.Time("snapshot_at").Default(time.Now),
		field.String("last_error").MaxLen(4000).Optional().Nillable(),
		field.Time("last_error_at").Optional().Nillable(),
		field.Int("shop_id").Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// AttributeContract is a versioned attribute snapshot.
// Cache key: description_category_id + type_id + language.
type AttributeContract struct {
	ent.Schema
}

func (AttributeContract) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "attribute_contracts"}}
}

func (AttributeContract) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("description_category_id"),
		field.Int64("type_id"),
		field.String("language").MaxLen(16).NotEmpty(),
		field.Int("version").Default(1),
		field.String("attributes_json").SchemaType(jsonb()).Default("[]"),
		field.String("source").MaxLen(64).Default(""),
		field.Time("synced_at").Default(time.Now),
	}
}

func (AttributeContract) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("description_category_id", "type_id", "language", "version").Unique(),
	}
}

// DictionarySnapshot caches dictionary values for one attribute, language and query.
type DictionarySnapshot struct {
	ent.Schema
}

func (DictionarySnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "dictionary_snapshots"}}
}

func (DictionarySnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("description_category_id"),
		field.Int64("type_id"),
		field.Int64("attribute_id"),
		field.String("language").MaxLen(16).NotEmpty(),
		field.String("query").MaxLen(500).Default(""),
		field.Int("version").Default(1),
		field.String("values_json").SchemaType(jsonb()).Default("[]"),
		field.Bool("has_next").Default(false),
		field.Int64("last_value_id").Default(0),
		field.Time("synced_at").Default(time.Now),
	}
}

func (DictionarySnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("description_category_id", "type_id", "attribute_id", "language", "query").Unique(),
	}
}

// CategoryPrompt is the Codex prompt for one category and leaf type.
// The key is the stable Ozon pair, so a tree resync does not drop the prompt.
type CategoryPrompt struct {
	ent.Schema
}

func (CategoryPrompt) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "category_prompts"}}
}

func (CategoryPrompt) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("description_category_id"),
		field.Int64("type_id"),
		field.String("codex_prompt").SchemaType(map[string]string{dialect.Postgres: "text"}).NotEmpty(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (CategoryPrompt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("description_category_id", "type_id").Unique(),
	}
}
