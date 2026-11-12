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

func textCol() map[string]string {
	return map[string]string{dialect.Postgres: "text"}
}

// WorkbenchTask is one 1688 material-processing job bound to a shop and a leaf type.
type WorkbenchTask struct {
	ent.Schema
}

func (WorkbenchTask) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "workbench_tasks"}}
}

func (WorkbenchTask) Fields() []ent.Field {
	return []ent.Field{
		field.Int("shop_id"),
		field.Int("category_node_id").Default(0),
		field.Int64("description_category_id"),
		field.Int64("type_id"),
		field.String("language").MaxLen(16).Default("ZH_HANS"),
		field.String("type_name").MaxLen(255).Default(""),
		field.JSON("type_path", []string{}).Optional(),
		field.String("master_sku").MaxLen(64).NotEmpty(),
		field.String("supplier_url").MaxLen(500).Default(""),
		field.String("zip_filename").MaxLen(255).Default(""),
		// Directory the zip was extracted into. Empty when extraction failed.
		field.String("zip_local_path").MaxLen(500).Default(""),
		field.Bool("auto_text_approve").Default(false),
		field.Bool("auto_visual_approve").Default(false),
		field.Bool("text_approved").Default(false),
		field.Bool("text_approved_auto").Default(false),
		field.Bool("visual_approved").Default(false),
		field.Bool("visual_approved_auto").Default(false),
		field.String("status").MaxLen(32).Default("RUNNING"),
		field.String("phase").MaxLen(32).Default("extract"),
		field.String("codex_session_id").MaxLen(128).Default(""),
		field.Int64("revision").Default(1),
		field.String("missing_fields").SchemaType(jsonb()).Default("[]"),
		field.String("input_values").SchemaType(jsonb()).Default("{}"),
		field.String("sub_skus").SchemaType(jsonb()).Default("[]"),
		field.String("text_draft").SchemaType(jsonb()).Default("{}"),
		field.String("visual_plan").SchemaType(jsonb()).Default("{}"),
		field.String("attributes_json").SchemaType(jsonb()).Default("[]"),
		field.String("prompt_template").SchemaType(textCol()).Default(""),
		field.String("rendered_prompt").SchemaType(textCol()).Default(""),
		field.String("error_message").MaxLen(4000).Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (WorkbenchTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("shop_id", "status"),
		index.Fields("status"),
	}
}

// TaskEvent is one append-only line in a task's Codex session timeline.
type TaskEvent struct {
	ent.Schema
}

func (TaskEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "task_events"}}
}

func (TaskEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("task_id"),
		field.Int("shop_id"),
		field.String("session_id").MaxLen(128).Default(""),
		field.Int("seq"),
		field.String("role").MaxLen(32).NotEmpty(),
		field.String("stage").MaxLen(64).Default(""),
		field.String("content").SchemaType(textCol()).Default(""),
		field.String("tool_call_id").MaxLen(128).Default(""),
		field.String("resource_ref").MaxLen(500).Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (TaskEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "seq").Unique(),
		index.Fields("shop_id", "task_id"),
	}
}
