package biz

import (
	"context"
	"encoding/json"
	"time"
)

// ListingGateway is the Seller API surface used to import items, read results and update stocks.
type ListingGateway interface {
	ImportProducts(ctx context.Context, cred OzonCred, body json.RawMessage) (taskID string, err error)
	ImportInfo(ctx context.Context, cred OzonCred, taskID string) ([]ImportOffer, error)
	ProductInfo(ctx context.Context, cred OzonCred, offerIDs []string) ([]PlatformCard, error)
	UpdateStocks(ctx context.Context, cred OzonCred, lines []StockLine) ([]StockLineResult, error)
}

// AcceptUnknownError means the import request timed out after it might have been accepted.
type AcceptUnknownError struct{ Msg string }

func (e *AcceptUnknownError) Error() string {
	if e == nil || e.Msg == "" {
		return "提交结果未知"
	}
	return e.Msg
}

// ImportOffer is one row of /v1/product/import/info.
type ImportOffer struct {
	OfferID   string
	ProductID int64
	Status    string
	Errors    []map[string]any
}

// PlatformCard is the latest read-back of one offer.
type PlatformCard struct {
	OfferID     string
	ProductID   int64
	SKU         int64
	Status      string
	Moderate    string
	Failed      string
	Name        string
	Description string
	Tooltip     string
	Archived    bool
}

// StockLine is one warehouse quantity sent to /v2/products/stocks.
type StockLine struct {
	OfferID     string
	ProductID   int64
	WarehouseID int64
	Stock       int
}

// StockLineResult is one row of the stocks response.
type StockLineResult struct {
	OfferID     string
	ProductID   int64
	WarehouseID int64
	Updated     bool
	Errors      []map[string]any
}

// ShopAccess is the shop surface the listing module needs.
type ShopAccess interface {
	Get(ctx context.Context, id int) (ShopView, error)
	Credentials(ctx context.Context, id int) (OzonCred, error)
	RememberCurrency(ctx context.Context, id int, code string) error
}

// ListingProduct groups SKUs that share a material task and a master SKU.
type ListingProduct struct {
	ID                    int
	ShopID                int
	TaskID                int
	MasterSKU             string
	SupplierURL           string
	DescriptionCategoryID int64
	TypeID                int64
	TypeName              string
	TypePath              []string
	Language              string
	Attributes            json.RawMessage
	CodexSessionID        string
}

// ListingSKU is one sellable offer.
type ListingSKU struct {
	ID                  int
	ShopID              int
	ProductID           int
	TaskID              int
	OfferID             string
	MasterSKU           string
	SupplierVariantRef  string
	VariantSpecs        string
	CategoryLabel       string
	CurrentVersion      int
	PublishStatus       string
	PlatformStatus      string
	PlatformReason      string
	LastImportedVersion int
	OzonProductID       int64
	OzonSKU             int64
	LastCheckedAt       *time.Time
	CurrencyCode        string
	Price               string
	TitleRU             string
	PrimaryImage        string
	PreviewPath         string
	LastError           string
	CodexAttempted      bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
	SupplierURL         string
	DescriptionCategory int64
	TypeID              int64
	TypeName            string
	Attributes          json.RawMessage
	SessionID           string
}

// ListingRevision is one stored import object.
type ListingRevision struct {
	ID        int
	SKUID     int
	ShopID    int
	Version   int
	Source    string
	CodexItem json.RawMessage
	ItemJSON  json.RawMessage
	Issues    []string
	Valid     bool
	CreatedAt time.Time
}

// ListingMedia is a local file and its public URL after upload.
type ListingMedia struct {
	ID         int
	SKUID      int
	ShopID     int
	Role       string
	LocalPath  string
	StorageKey string
	PublicURL  string
	PublicKey  string
	Status     string
	Error      string
	Attempts   int
}

// PublishBatch is one operator action, possibly split into several HTTP calls.
type PublishBatch struct {
	ID             int
	ShopID         int
	IdempotencyKey string
	SelectionHash  string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Requests       []PublishRequest
}

// PublishRequest is one POST /v3/product/import body.
type PublishRequest struct {
	ID       int
	BatchID  int
	ShopID   int
	Chunk    int
	Body     json.RawMessage
	TaskID   string
	Status   string
	Error    string
	SentAt   *time.Time
	Finished *time.Time
	Items    []PublishItem
}

// PublishItem is one offer inside a request. Results stay on this row.
type PublishItem struct {
	ID          int
	RequestID   int
	BatchID     int
	SKUID       int
	ShopID      int
	Version     int
	OfferID     string
	Result      string
	ProductID   int64
	OzonSKU     int64
	ErrorCode   string
	ErrorDetail string
}

// SKUPatch is a conditional update applied with a batch result.
// Publish status changes only when ExpectVersion is the SKU's current version.
type SKUPatch struct {
	ID                  int
	ExpectVersion       int
	PublishStatus       *string
	PlatformStatus      *string
	PlatformReason      *string
	LastImportedVersion *int
	OzonProductID       *int64
	OzonSKU             *int64
	LastError           *string
	Checked             bool
}

// WarehouseStock is the target quantity for one warehouse.
type WarehouseStock struct {
	ID          int
	SKUID       int
	ShopID      int
	WarehouseID int64
	TargetQty   int
	PlatformQty int
	SyncStatus  string
	SyncMessage string
	SyncedAt    *time.Time
}

// ListingFilter is the server-side SKU query.
type ListingFilter struct {
	ShopID   int
	Mode     string
	Query    string
	Offers   []string
	Publish  []string
	Platform string
	Page     int
	PageSize int
}

// ListingRepo persists SKUs, revisions, batches and stocks.
type ListingRepo interface {
	SaveProduct(ctx context.Context, p *ListingProduct) (*ListingProduct, error)
	FindProduct(ctx context.Context, shopID, taskID int, master string) (*ListingProduct, error)
	GetProduct(ctx context.Context, shopID, id int) (*ListingProduct, error)
	FindOffer(ctx context.Context, shopID int, offer string) (*ListingSKU, error)
	CreateSKU(ctx context.Context, sku *ListingSKU, rev *ListingRevision, media []ListingMedia) (*ListingSKU, error)
	GetSKU(ctx context.Context, shopID, id int) (*ListingSKU, error)
	ListSKUs(ctx context.Context, f ListingFilter) ([]ListingSKU, int, error)
	ListRevisions(ctx context.Context, shopID, skuID int) ([]ListingRevision, error)
	GetRevision(ctx context.Context, shopID, skuID, version int) (*ListingRevision, error)
	AddRevision(ctx context.Context, sku *ListingSKU, rev *ListingRevision) (*ListingSKU, error)
	ListMedia(ctx context.Context, skuID int) ([]ListingMedia, error)
	SaveMedia(ctx context.Context, row *ListingMedia) (*ListingMedia, error)
	LookupBatch(ctx context.Context, shopID int, key string) (*PublishBatch, error)
	CreateBatch(ctx context.Context, batch *PublishBatch) (*PublishBatch, error)
	GetBatch(ctx context.Context, shopID, id int) (*PublishBatch, error)
	SaveBatch(ctx context.Context, batch *PublishBatch, patches []SKUPatch) error
	ListInflight(ctx context.Context) ([]*PublishBatch, error)
	SaveStock(ctx context.Context, row *WarehouseStock) (*WarehouseStock, error)
	ListStocks(ctx context.Context, shopID, skuID int) ([]WarehouseStock, error)
	ListCompletedTasks(ctx context.Context) ([]*Task, error)
}
