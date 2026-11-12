package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ListingProduct groups sellable SKUs that share a material task and a master SKU.
type ListingProduct struct {
	ent.Schema
}

func (ListingProduct) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "listing_products"}}
}

func (ListingProduct) Fields() []ent.Field {
	return []ent.Field{
		field.Int("shop_id"),
		field.Int("task_id").Default(0),
		field.String("master_sku").MaxLen(64).NotEmpty(),
		field.String("supplier_url").MaxLen(500).Default(""),
		field.Int64("description_category_id").Default(0),
		field.Int64("type_id").Default(0),
		field.String("type_name").MaxLen(255).Default(""),
		field.JSON("type_path", []string{}).Optional(),
		field.String("language").MaxLen(16).Default("ZH_HANS"),
		field.String("attributes_json").SchemaType(jsonb()).Default("[]"),
		field.String("codex_session_id").MaxLen(128).Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ListingProduct) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("shop_id", "task_id", "master_sku").Unique(),
		index.Fields("shop_id", "master_sku"),
	}
}

// ListingSKU is one sellable offer inside a shop.
type ListingSKU struct {
	ent.Schema
}

func (ListingSKU) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "listing_skus"}}
}

func (ListingSKU) Fields() []ent.Field {
	return []ent.Field{
		field.Int("shop_id"),
		field.Int("product_id"),
		field.Int("task_id").Default(0),
		field.String("offer_id").MaxLen(128).NotEmpty(),
		field.String("master_sku").MaxLen(64).Default(""),
		field.String("supplier_variant_ref").MaxLen(128).Default(""),
		field.String("variant_specs").MaxLen(500).Default(""),
		field.String("category_label").MaxLen(500).Default(""),
		field.Int("current_version").Default(1),
		field.String("publish_status").MaxLen(32).Default("待完善"),
		field.String("platform_status").MaxLen(32).Default("待回读"),
		field.String("platform_reason").MaxLen(2000).Default(""),
		field.Int("last_imported_version").Default(0),
		field.Int64("ozon_product_id").Default(0),
		field.Int64("ozon_sku").Default(0),
		field.Time("last_checked_at").Optional().Nillable(),
		field.String("currency_code").MaxLen(3).Default(""),
		field.String("price").MaxLen(32).Default(""),
		field.String("title_ru").MaxLen(1000).Default(""),
		field.String("primary_image").MaxLen(2000).Default(""),
		field.String("preview_path").MaxLen(500).Default(""),
		field.String("last_error").MaxLen(4000).Default(""),
		field.Bool("codex_attempted").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ListingSKU) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("shop_id", "offer_id").Unique(),
		index.Fields("shop_id", "publish_status", "id"),
		index.Fields("product_id"),
	}
}

// ListingRevision is one version of the import item object.
type ListingRevision struct {
	ent.Schema
}

func (ListingRevision) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "listing_sku_revisions"}}
}

func (ListingRevision) Fields() []ent.Field {
	return []ent.Field{
		field.Int("listing_sku_id"),
		field.Int("shop_id"),
		field.Int("version"),
		field.String("source").MaxLen(32).Default("draft"),
		field.String("codex_item_json").SchemaType(jsonb()).Default("{}"),
		field.String("item_json").SchemaType(jsonb()).Default("{}"),
		field.String("contract_version").MaxLen(64).Default(""),
		field.String("validation_json").SchemaType(jsonb()).Default("[]"),
		field.Bool("valid").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (ListingRevision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("listing_sku_id", "version").Unique(),
	}
}

// ListingMedia is one local file and, after upload, its public URL.
type ListingMedia struct {
	ent.Schema
}

func (ListingMedia) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "listing_media"}}
}

func (ListingMedia) Fields() []ent.Field {
	return []ent.Field{
		field.Int("listing_sku_id"),
		field.Int("shop_id"),
		field.String("role").MaxLen(32).Default("image"),
		field.String("local_path").MaxLen(500).Default(""),
		field.String("storage_key").MaxLen(500).Default(""),
		field.String("public_url").MaxLen(2000).Default(""),
		field.String("public_key").MaxLen(2000).Default(""),
		field.String("status").MaxLen(32).Default("local"),
		field.String("error_message").MaxLen(2000).Default(""),
		field.Int("attempts").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ListingMedia) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("listing_sku_id"),
	}
}

// PublishBatch is one operator action. The idempotency key is unique per shop.
type PublishBatch struct {
	ent.Schema
}

func (PublishBatch) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "publish_batches"}}
}

func (PublishBatch) Fields() []ent.Field {
	return []ent.Field{
		field.Int("shop_id"),
		field.String("idempotency_key").MaxLen(80).NotEmpty(),
		field.String("selection_hash").MaxLen(80).Default(""),
		field.String("status").MaxLen(32).Default("pending"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PublishBatch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("shop_id", "idempotency_key").Unique(),
	}
}

// PublishRequest is one HTTP call, at most 100 items, with the exact body that was sent.
type PublishRequest struct {
	ent.Schema
}

func (PublishRequest) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "publish_requests"}}
}

func (PublishRequest) Fields() []ent.Field {
	return []ent.Field{
		field.Int("batch_id"),
		field.Int("shop_id"),
		field.Int("chunk_index").Default(0),
		field.String("request_body_json").SchemaType(jsonb()).Default("{}"),
		field.String("task_id").MaxLen(64).Default(""),
		field.String("status").MaxLen(32).Default("pending"),
		field.String("error_message").MaxLen(2000).Default(""),
		field.Time("sent_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
	}
}

func (PublishRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id", "chunk_index").Unique(),
		index.Fields("status"),
	}
}

// PublishItem is one offer inside a batch. A late result stays on this row.
type PublishItem struct {
	ent.Schema
}

func (PublishItem) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "publish_items"}}
}

func (PublishItem) Fields() []ent.Field {
	return []ent.Field{
		field.Int("request_id"),
		field.Int("batch_id"),
		field.Int("listing_sku_id"),
		field.Int("shop_id"),
		field.Int("version"),
		field.String("offer_id").MaxLen(128).NotEmpty(),
		field.String("result_status").MaxLen(32).Default("pending"),
		field.Int64("ozon_product_id").Default(0),
		field.Int64("ozon_sku").Default(0),
		field.String("error_code").MaxLen(128).Default(""),
		field.String("error_detail").MaxLen(4000).Default(""),
	}
}

func (PublishItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id", "listing_sku_id").Unique(),
		index.Fields("offer_id"),
	}
}

// SKUWarehouseStock is the target quantity for one SKU on one warehouse.
type SKUWarehouseStock struct {
	ent.Schema
}

func (SKUWarehouseStock) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "sku_warehouse_stocks"}}
}

func (SKUWarehouseStock) Fields() []ent.Field {
	return []ent.Field{
		field.Int("listing_sku_id"),
		field.Int("shop_id"),
		field.Int64("warehouse_id"),
		field.Int("target_qty").Default(0),
		field.Int("platform_qty").Default(-1),
		field.String("sync_status").MaxLen(32).Default(""),
		field.String("sync_message").MaxLen(2000).Default(""),
		field.Time("synced_at").Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SKUWarehouseStock) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("listing_sku_id", "warehouse_id").Unique(),
	}
}
