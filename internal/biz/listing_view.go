package biz

import (
	"encoding/json"
	"time"
)

// ListingRow is one table line.
type ListingRow struct {
	ID                  int       `json:"id"`
	ShopID              int       `json:"shop_id"`
	TaskID              int       `json:"task_id"`
	OfferID             string    `json:"offer_id"`
	MasterSKU           string    `json:"master_sku"`
	VariantSpecs        string    `json:"variant_specs"`
	Title               string    `json:"title"`
	CategoryLabel       string    `json:"category_label"`
	TypeID              int64     `json:"type_id"`
	Price               string    `json:"price"`
	CurrencyCode        string    `json:"currency_code"`
	PrimaryImage        string    `json:"primary_image"`
	PreviewURL          string    `json:"preview_url"`
	PublishStatus       string    `json:"publish_status"`
	PlatformStatus      string    `json:"platform_status"`
	PlatformReason      string    `json:"platform_reason"`
	CurrentVersion      int       `json:"current_version"`
	LastImportedVersion int       `json:"last_imported_version"`
	SupplierURL         string    `json:"supplier_url"`
	UpdatedAt           time.Time `json:"updated_at"`
	LastError           string    `json:"last_error"`
	Eligible            bool      `json:"eligible"`
	OzonProductID       int64     `json:"ozon_product_id"`
	OzonSKU             int64     `json:"ozon_sku"`
}

// ListingPage is a filtered page.
type ListingPage struct {
	Rows     []ListingRow `json:"rows"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// RevisionView is one saved version.
type RevisionView struct {
	Version   int             `json:"version"`
	Source    string          `json:"source"`
	Valid     bool            `json:"valid"`
	Issues    []string        `json:"issues"`
	ItemJSON  json.RawMessage `json:"item_json"`
	CreatedAt time.Time       `json:"created_at"`
}

// MediaView is one image or video on the detail page.
type MediaView struct {
	ID        int    `json:"id"`
	Role      string `json:"role"`
	LocalPath string `json:"local_path"`
	PublicURL string `json:"public_url"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	Attempts  int    `json:"attempts"`
	Preview   string `json:"preview"`
}

// StockView is one warehouse row.
type StockView struct {
	WarehouseID int64      `json:"warehouse_id"`
	TargetQty   int        `json:"target_qty"`
	PlatformQty int        `json:"platform_qty"`
	SyncStatus  string     `json:"sync_status"`
	SyncMessage string     `json:"sync_message"`
	SyncedAt    *time.Time `json:"synced_at"`
}

// StorageView is safe to show. It never includes a token.
type StorageView struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
	Message    string `json:"message"`
}

// ListingDetail is the dynamic form payload.
type ListingDetail struct {
	SKU       ListingRow      `json:"sku"`
	ItemJSON  json.RawMessage `json:"item_json"`
	CodexJSON json.RawMessage `json:"codex_item_json"`
	Version   int             `json:"version"`
	Source    string          `json:"source"`
	Issues    []string        `json:"issues"`
	Contract  json.RawMessage `json:"contract"`
	Revisions []RevisionView  `json:"revisions"`
	Media     []MediaView     `json:"media"`
	Stocks    []StockView     `json:"stocks"`
	Storage   StorageView     `json:"storage"`
}

// PublishCheck is one selected SKU in a preflight.
type PublishCheck struct {
	ID      int      `json:"id"`
	OfferID string   `json:"offer_id"`
	Version int      `json:"version"`
	OK      bool     `json:"ok"`
	Errors  []string `json:"errors"`
}

// BatchView is the stored batch and its per-offer results.
type BatchView struct {
	ID             int           `json:"id"`
	ShopID         int           `json:"shop_id"`
	IdempotencyKey string        `json:"idempotency_key"`
	Status         string        `json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	Requests       []RequestView `json:"requests"`
	Imported       int           `json:"imported"`
	Failed         int           `json:"failed"`
	Pending        int           `json:"pending"`
	Unknown        int           `json:"unknown"`
}

// RequestView is one saved HTTP body.
type RequestView struct {
	ID     int             `json:"id"`
	Chunk  int             `json:"chunk"`
	TaskID string          `json:"task_id"`
	Status string          `json:"status"`
	Error  string          `json:"error"`
	Body   json.RawMessage `json:"request_body"`
	Items  []ItemResult    `json:"items"`
}

// ItemResult is the write-back for one offer.
type ItemResult struct {
	SKUID       int    `json:"listing_sku_id"`
	OfferID     string `json:"offer_id"`
	Version     int    `json:"version"`
	Result      string `json:"result"`
	ProductID   int64  `json:"product_id"`
	OzonSKU     int64  `json:"ozon_sku"`
	ErrorCode   string `json:"error_code"`
	ErrorDetail string `json:"error_detail"`
}

// PublishPlan is the preflight, and the batch when the call also submits.
type PublishPlan struct {
	Ready       []PublishCheck    `json:"ready"`
	Blocked     []PublishCheck    `json:"blocked"`
	Chunks      int               `json:"chunks"`
	Bodies      []json.RawMessage `json:"bodies"`
	NeedConfirm bool              `json:"need_confirm"`
	Batch       *BatchView        `json:"batch,omitempty"`
}

// PublishInput is the shared body for preview and submit.
type PublishInput struct {
	ShopID         int
	Items          []PublishPick
	IdempotencyKey string
	OnlyValid      bool
	Execute        bool
}

// PublishPick is one explicit SKU version.
type PublishPick struct {
	ID      int `json:"id"`
	Version int `json:"version"`
}

func (s ListingSKU) Row() ListingRow {
	preview := s.PreviewPath
	if IsPublicHTTPS(s.PrimaryImage) {
		preview = s.PrimaryImage
	}
	return ListingRow{
		ID: s.ID, ShopID: s.ShopID, TaskID: s.TaskID, OfferID: s.OfferID, MasterSKU: s.MasterSKU,
		VariantSpecs: s.VariantSpecs, Title: s.TitleRU, CategoryLabel: s.CategoryLabel, TypeID: s.TypeID,
		Price: s.Price, CurrencyCode: s.CurrencyCode, PrimaryImage: s.PrimaryImage, PreviewURL: preview,
		PublishStatus: s.PublishStatus, PlatformStatus: s.PlatformStatus, PlatformReason: s.PlatformReason,
		CurrentVersion: s.CurrentVersion, LastImportedVersion: s.LastImportedVersion, SupplierURL: s.SupplierURL,
		UpdatedAt: s.UpdatedAt, LastError: s.LastError, OzonProductID: s.OzonProductID, OzonSKU: s.OzonSKU,
		Eligible: Eligible(s.PublishStatus, s.CurrentVersion, s.LastImportedVersion),
	}
}

func revisionViews(revs []ListingRevision) []RevisionView {
	out := make([]RevisionView, 0, len(revs))
	for _, rev := range revs {
		issues := rev.Issues
		if issues == nil {
			issues = []string{}
		}
		out = append(out, RevisionView{
			Version: rev.Version, Source: rev.Source, Valid: rev.Valid, Issues: issues,
			ItemJSON: rawOr(rev.ItemJSON, []byte("{}")), CreatedAt: rev.CreatedAt,
		})
	}
	return out
}

func mediaViews(rows []ListingMedia, taskID int) []MediaView {
	out := make([]MediaView, 0, len(rows))
	for _, row := range rows {
		preview := row.PublicURL
		if preview == "" && taskID > 0 && row.LocalPath != "" {
			preview = "/media/tasks/" + itoa(taskID) + "/" + baseName(row.LocalPath)
		}
		out = append(out, MediaView{
			ID: row.ID, Role: row.Role, LocalPath: row.LocalPath, PublicURL: row.PublicURL,
			Status: row.Status, Error: row.Error, Attempts: row.Attempts, Preview: preview,
		})
	}
	return out
}

func stockViews(rows []WarehouseStock) []StockView {
	out := make([]StockView, 0, len(rows))
	for _, row := range rows {
		out = append(out, StockView{
			WarehouseID: row.WarehouseID, TargetQty: row.TargetQty, PlatformQty: row.PlatformQty,
			SyncStatus: row.SyncStatus, SyncMessage: row.SyncMessage, SyncedAt: row.SyncedAt,
		})
	}
	return out
}

func (b *PublishBatch) View() BatchView {
	if b == nil {
		return BatchView{}
	}
	view := BatchView{
		ID: b.ID, ShopID: b.ShopID, IdempotencyKey: b.IdempotencyKey, Status: b.Status, CreatedAt: b.CreatedAt,
		Requests: []RequestView{},
	}
	for _, req := range b.Requests {
		rv := RequestView{
			ID: req.ID, Chunk: req.Chunk, TaskID: req.TaskID, Status: req.Status, Error: req.Error,
			Body: rawOr(req.Body, []byte("{}")), Items: []ItemResult{},
		}
		for _, item := range req.Items {
			switch item.Result {
			case string(RemoteImported):
				view.Imported++
			case string(RemoteFailed):
				view.Failed++
			case string(RemoteUnknown):
				view.Unknown++
			default:
				view.Pending++
			}
			rv.Items = append(rv.Items, ItemResult{
				SKUID: item.SKUID, OfferID: item.OfferID, Version: item.Version, Result: item.Result,
				ProductID: item.ProductID, OzonSKU: item.OzonSKU, ErrorCode: item.ErrorCode, ErrorDetail: item.ErrorDetail,
			})
		}
		view.Requests = append(view.Requests, rv)
	}
	return view
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
