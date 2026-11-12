package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Warehouse is cached from POST /v2/warehouse/list.
type Warehouse struct {
	WarehouseID   int64  `json:"warehouse_id"`
	Name          string `json:"name"`
	Status        string `json:"status,omitempty"`
	IsRFBS        bool   `json:"is_rfbs"`
	WarehouseType string `json:"warehouse_type,omitempty"`
}

// Shop holds encrypted seller credentials.
type Shop struct {
	ent.Schema
}

func (Shop) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "shops"}}
}

func (Shop) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.String("client_id").MaxLen(64).NotEmpty().Unique(),
		field.String("currency_code").MaxLen(3).Optional().Nillable(),
		field.Bytes("api_key_ciphertext"),
		field.JSON("warehouses", []Warehouse{}).Optional(),
		// warehouses_synced_at is when the current list was last returned by a successful call.
		// A failed test does not move this timestamp.
		field.Time("warehouses_synced_at").Optional().Nillable(),
		// warehouses_mock is true only when the stored list came from the in-process sample client.
		field.Bool("warehouses_mock").Default(false),
		field.Bool("is_default").Default(false),
		field.Bool("archived").Default(false),
		field.Time("last_tested_at").Optional().Nillable(),
		field.Bool("last_test_ok").Optional().Nillable(),
		field.String("last_test_message").MaxLen(2000).Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Shop) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("archived"),
	}
}
