package biz

import (
	"context"
	"time"

	"ozon-workbench/internal/catalog"
)

// OzonCred is injected per shop. It is never written to API responses or logs.
type OzonCred struct {
	ClientID string
	APIKey   string
}

// Warehouse is the subset of POST /v2/warehouse/list kept on the shop.
type Warehouse struct {
	WarehouseID   int64  `json:"warehouse_id"`
	Name          string `json:"name"`
	Status        string `json:"status,omitempty"`
	IsRFBS        bool   `json:"is_rfbs"`
	WarehouseType string `json:"warehouse_type,omitempty"`
}

// ValuePage is one page of POST /v1/description-category/attribute/values.
type ValuePage struct {
	Values      []map[string]any
	HasNext     bool
	LastValueID int64
}

// OzonGateway is the Seller API surface used by phase 1.
type OzonGateway interface {
	ListWarehouses(ctx context.Context, cred OzonCred) ([]Warehouse, error)
	CategoryTree(ctx context.Context, cred OzonCred, language string) ([]catalog.TreeNode, error)
	Attributes(ctx context.Context, cred OzonCred, categoryID, typeID int64, language string) ([]map[string]any, error)
	AttributeValues(ctx context.Context, cred OzonCred, categoryID, typeID, attributeID, lastValueID int64, limit int, language string) (*ValuePage, error)
	SearchAttributeValues(ctx context.Context, cred OzonCred, categoryID, typeID, attributeID int64, value string, limit int) ([]map[string]any, error)
}

// Cipher encrypts shop API keys at rest.
type Cipher interface {
	Encrypt(plain string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

// Shop is the persisted shop. APIKeyCiphertext stays inside the process.
type Shop struct {
	ID               int
	Name             string
	ClientID         string
	CurrencyCode     *string
	APIKeyCiphertext []byte
	Warehouses       []Warehouse
	IsDefault        bool
	Archived         bool
	LastTestedAt     *time.Time
	LastTestOK       *bool
	LastTestMessage  *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ShopView is the HTTP shape. It has no key material.
type ShopView struct {
	ID               int         `json:"id"`
	Name             string      `json:"name"`
	ClientID         string      `json:"client_id"`
	CurrencyCode     *string     `json:"currency_code"`
	APIKeyConfigured bool        `json:"api_key_configured"`
	Warehouses       []Warehouse `json:"warehouses"`
	IsDefault        bool        `json:"is_default"`
	Archived         bool        `json:"archived"`
	LastTestedAt     *time.Time  `json:"last_tested_at"`
	LastTestOK       *bool       `json:"last_test_ok"`
	LastTestMessage  *string     `json:"last_test_message"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// View strips secrets. Currency stays null until a later quoting step records it.
func (s *Shop) View() ShopView {
	wh := s.Warehouses
	if wh == nil {
		wh = []Warehouse{}
	}
	return ShopView{
		ID:               s.ID,
		Name:             s.Name,
		ClientID:         s.ClientID,
		CurrencyCode:     s.CurrencyCode,
		APIKeyConfigured: len(s.APIKeyCiphertext) > 0,
		Warehouses:       wh,
		IsDefault:        s.IsDefault,
		Archived:         s.Archived,
		LastTestedAt:     s.LastTestedAt,
		LastTestOK:       s.LastTestOK,
		LastTestMessage:  s.LastTestMessage,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

// ConnectionResult is returned after a credential check.
type ConnectionResult struct {
	OK             bool   `json:"ok"`
	Message        string `json:"message"`
	WarehouseCount int    `json:"warehouse_count"`
}

// ShopWriteResult is a saved shop plus the optional live check.
type ShopWriteResult struct {
	Shop       ShopView          `json:"shop"`
	Connection *ConnectionResult `json:"connection,omitempty"`
}

// ShopRepo is the shop persistence port.
type ShopRepo interface {
	List(ctx context.Context) ([]*Shop, error)
	Get(ctx context.Context, id int) (*Shop, error)
	GetByClientID(ctx context.Context, clientID string) (*Shop, error)
	Create(ctx context.Context, shop *Shop) (*Shop, error)
	Update(ctx context.Context, shop *Shop) (*Shop, error)
	SetDefault(ctx context.Context, id int) error
}
