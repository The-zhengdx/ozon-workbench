// Package domain 是 V2 表结构在 Go 里的形态。字段与 docs/requirements-v2/03_数据库参考DDL.sql 对齐。
package domain

import (
	"encoding/json"
	"time"
)

const (
	ShopCurrencyDefault = "CNY"
	ShopVATDefault      = "0.1"
)

// ShopColumns 是 ozon_shops 的全部列，顺序固定，测试用来卡住“只能有 9 字段”。
func ShopColumns() []string {
	return []string{
		"id", "name", "client_id", "api_key", "currency_code", "vat", "enable", "created_at", "updated_at",
	}
}

type Shop struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	ClientID     string    `json:"client_id"`
	APIKey       string    `json:"api_key,omitempty"`
	CurrencyCode string    `json:"currency_code"`
	VAT          string    `json:"vat"`
	Enable       bool      `json:"enable"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Rule struct {
	ID                    int64     `json:"id"`
	DescriptionCategoryID int64     `json:"description_category_id"`
	TypeID                int64     `json:"type_id"`
	Prompt                string    `json:"prompt"`
	Enabled               bool      `json:"enabled"`
	Version               int       `json:"version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type Task struct {
	ID                         int64           `json:"id"`
	ShopID                     int64           `json:"shop_id"`
	DescriptionCategoryID      int64           `json:"description_category_id"`
	TypeID                     int64           `json:"type_id"`
	BaseSKU                    string          `json:"base_sku"`
	OriginLink                 string          `json:"origin_link,omitempty"`
	CategoryRuleID             *int64          `json:"category_rule_id,omitempty"`
	CategoryRuleVersion        *int            `json:"category_rule_version,omitempty"`
	PromptSnapshot             string          `json:"prompt_snapshot"`
	CodexSessionID             string          `json:"codex_session_id,omitempty"`
	CategoryToken              string          `json:"category_token,omitempty"`
	Status                     string          `json:"status"`
	Stage                      string          `json:"stage"`
	RequirementRevision        int64           `json:"requirement_revision"`
	AppliedRequirementRevision int64           `json:"applied_requirement_revision"`
	CopyAutoApprove            bool            `json:"copy_auto_approve"`
	VisualPlanAutoApprove      bool            `json:"visual_plan_auto_approve"`
	VisualResultAutoApprove    bool            `json:"visual_result_auto_approve"`
	CopyApproved               bool            `json:"copy_approved"`
	VisualPlanApproved         bool            `json:"visual_plan_approved"`
	VisualResultApproved       bool            `json:"visual_result_approved"`
	LastReplyJSON              json.RawMessage `json:"last_reply_json,omitempty"`
	LastError                  string          `json:"last_error,omitempty"`
	AdmittedAt                 *time.Time      `json:"admitted_at,omitempty"`
	FinishedAt                 *time.Time      `json:"finished_at,omitempty"`
	CancelRequested            bool            `json:"cancel_requested"`
	CreatedAt                  time.Time       `json:"created_at"`
	UpdatedAt                  time.Time       `json:"updated_at"`
}

const (
	StatusQueued         = "queued"
	StatusDispatching    = "dispatching"
	StatusRunning        = "running"
	StatusWaitingInput   = "waiting_input"
	StatusWaitingReview  = "waiting_review"
	StatusUploadingMedia = "uploading_media"
	StatusFinalizing     = "finalizing"
	StatusCompleted      = "completed"
	StatusFailed         = "failed"
	StatusCancelled      = "cancelled"
)

func ActiveStatuses() []string {
	return []string{
		StatusDispatching, StatusRunning, StatusWaitingInput, StatusWaitingReview,
		StatusUploadingMedia, StatusFinalizing,
	}
}

func HoldsSlot(status string) bool {
	switch status {
	case StatusDispatching, StatusRunning, StatusWaitingInput, StatusWaitingReview, StatusUploadingMedia, StatusFinalizing:
		return true
	default:
		return false
	}
}

type Turn struct {
	ID                          int64           `json:"id"`
	TaskID                      int64           `json:"task_id"`
	Seq                         int             `json:"seq"`
	RunStatus                   string          `json:"run_status"`
	InputMaxRequirementRevision int64           `json:"input_max_requirement_revision"`
	OutputFile                  string          `json:"output_file,omitempty"`
	ExitCode                    *int            `json:"exit_code,omitempty"`
	ReplyJSON                   json.RawMessage `json:"reply_json,omitempty"`
	ErrorMessage                string          `json:"error_message,omitempty"`
	StartedAt                   time.Time       `json:"started_at"`
	FinishedAt                  *time.Time      `json:"finished_at,omitempty"`
}

const (
	TurnStarting  = "starting"
	TurnRunning   = "running"
	TurnSucceeded = "succeeded"
	TurnFailed    = "failed"
	TurnUncertain = "uncertain"
)

type Message struct {
	ID                  int64           `json:"id"`
	TaskID              int64           `json:"task_id"`
	TurnID              *int64          `json:"turn_id,omitempty"`
	Role                string          `json:"role"`
	Kind                string          `json:"kind"`
	Content             string          `json:"content"`
	Payload             json.RawMessage `json:"payload,omitempty"`
	RequirementRevision *int64          `json:"requirement_revision,omitempty"`
	DeliveryStatus      string          `json:"delivery_status"`
	CreatedAt           time.Time       `json:"created_at"`
	ProcessedAt         *time.Time      `json:"processed_at,omitempty"`
}

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"

	KindUserRequirement = "user_requirement"
	KindUserInput       = "user_input"
	KindReviewDecision  = "review_decision"
	KindMediaUploaded   = "media_uploaded"
	KindCodexReply      = "codex_reply"
	KindSystemNote      = "system_note"

	DeliveryPending       = "pending"
	DeliverySent          = "sent"
	DeliveryAccepted      = "accepted"
	DeliveryUncertain     = "uncertain"
	DeliveryNotApplicable = "not_applicable"
)

type Asset struct {
	ID           int64     `json:"id"`
	TaskID       int64     `json:"task_id"`
	AssetID      string    `json:"asset_id"`
	SKURef       string    `json:"sku_ref"`
	Role         string    `json:"role"`
	LocalPath    string    `json:"local_path"`
	ObjectKey    string    `json:"object_key,omitempty"`
	PublicURL    string    `json:"public_url,omitempty"`
	MimeType     string    `json:"mime_type,omitempty"`
	UploadStatus string    `json:"upload_status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const (
	UploadPending    = "pending"
	UploadUploading  = "uploading"
	UploadUploaded   = "uploaded"
	UploadFailed     = "failed"
	UploadSuperseded = "superseded"
)

type Delivery struct {
	TaskID      int64     `json:"task_id"`
	SKURef      string    `json:"sku_ref"`
	ProductID   int64     `json:"product_id"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type Product struct {
	ID                       int64           `json:"id"`
	ShopID                   int64           `json:"shop_id"`
	GenerationTaskID         *int64          `json:"generation_task_id,omitempty"`
	BaseSKU                  string          `json:"base_sku"`
	OfferID                  *string         `json:"offer_id,omitempty"`
	VariantLabel             *string         `json:"variant_label,omitempty"`
	OriginLink               *string         `json:"origin_link,omitempty"`
	DescriptionCategoryID    int64           `json:"description_category_id"`
	TypeID                   int64           `json:"type_id"`
	NewDescriptionCategoryID *int64          `json:"new_description_category_id,omitempty"`
	Name                     *string         `json:"name,omitempty"`
	Barcode                  *string         `json:"barcode,omitempty"`
	Cost                     *string         `json:"cost,omitempty"`
	CostCurrencyCode         string          `json:"cost_currency_code"`
	CostSourceJSON           json.RawMessage `json:"cost_source_json"`
	Price                    *string         `json:"price,omitempty"`
	OldPrice                 *string         `json:"old_price,omitempty"`
	CurrencyCode             *string         `json:"currency_code,omitempty"`
	VAT                      *string         `json:"vat,omitempty"`
	PricingRuleID            *int64          `json:"pricing_rule_id,omitempty"`
	PricingSnapshot          json.RawMessage `json:"pricing_snapshot"`
	PrimaryImage             *string         `json:"primary_image,omitempty"`
	Images                   json.RawMessage `json:"images"`
	ColorImage               *string         `json:"color_image,omitempty"`
	PDFList                  json.RawMessage `json:"pdf_list"`
	Depth                    *int            `json:"depth,omitempty"`
	Height                   *int            `json:"height,omitempty"`
	Width                    *int            `json:"width,omitempty"`
	DimensionUnit            string          `json:"dimension_unit"`
	Weight                   *int            `json:"weight,omitempty"`
	WeightUnit               string          `json:"weight_unit"`
	Promotions               json.RawMessage `json:"promotions"`
	Attributes               json.RawMessage `json:"attributes"`
	ComplexAttributes        json.RawMessage `json:"complex_attributes"`
	AIStructuredJSON         json.RawMessage `json:"ai_structured_json,omitempty"`
	APIItemJSON              json.RawMessage `json:"api_item_json,omitempty"`
	APIJSONRevision          int             `json:"api_json_revision"`
	LastImportedRevision     *int            `json:"last_imported_revision,omitempty"`
	ValidationStatus         string          `json:"validation_status"`
	ValidationErrors         json.RawMessage `json:"validation_errors"`
	TextReviewStatus         string          `json:"text_review_status"`
	ImageReviewStatus        string          `json:"image_review_status"`
	PublishStatus            string          `json:"publish_status"`
	OzonModerationStatus     *string         `json:"ozon_moderation_status,omitempty"`
	OzonSaleStatus           *string         `json:"ozon_sale_status,omitempty"`
	OzonProductID            *int64          `json:"ozon_product_id,omitempty"`
	OzonSKU                  *int64          `json:"ozon_sku,omitempty"`
	OzonStatusRaw            json.RawMessage `json:"ozon_status_raw"`
	LastErrorJSON            json.RawMessage `json:"last_error_json"`
	LastSubmittedAt          *time.Time      `json:"last_submitted_at,omitempty"`
	LastImportedAt           *time.Time      `json:"last_imported_at,omitempty"`
	LastPlatformSyncedAt     *time.Time      `json:"last_platform_synced_at,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

// ProductFieldCount 是需求里的 54 个字段（含主键和时间戳）。
func ProductFieldCount() int { return 54 }

type PricingRule struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Enabled        bool            `json:"enabled"`
	ParametersJSON json.RawMessage `json:"parameters_json"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ImportBatch struct {
	ID           int64           `json:"id"`
	ShopID       int64           `json:"shop_id"`
	OzonTaskID   *int64          `json:"ozon_task_id,omitempty"`
	Status       string          `json:"status"`
	RequestJSON  json.RawMessage `json:"request_json"`
	ResponseJSON json.RawMessage `json:"response_json,omitempty"`
	LastError    json.RawMessage `json:"last_error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	SubmittedAt  *time.Time      `json:"submitted_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
}

type ImportBatchItem struct {
	ID                int64           `json:"id"`
	BatchID           int64           `json:"batch_id"`
	ProductID         int64           `json:"product_id"`
	OfferID           string          `json:"offer_id"`
	SubmittedRevision int             `json:"submitted_revision"`
	RequestItemJSON   json.RawMessage `json:"request_item_json"`
	Status            string          `json:"status"`
	OzonProductID     *int64          `json:"ozon_product_id,omitempty"`
	ErrorJSON         json.RawMessage `json:"error_json,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type DictValue struct {
	ShopID                int64     `json:"shop_id"`
	DescriptionCategoryID int64     `json:"description_category_id"`
	TypeID                int64     `json:"type_id"`
	AttributeID           int64     `json:"attribute_id"`
	Language              string    `json:"language"`
	ValueID               int64     `json:"value_id"`
	ValueText             string    `json:"value_text"`
	Info                  string    `json:"info,omitempty"`
	Picture               string    `json:"picture,omitempty"`
	Source                string    `json:"source"`
	FetchedAt             time.Time `json:"fetched_at"`
}

type DictSync struct {
	ShopID                int64      `json:"shop_id"`
	DescriptionCategoryID int64      `json:"description_category_id"`
	TypeID                int64      `json:"type_id"`
	AttributeID           int64      `json:"attribute_id"`
	Language              string     `json:"language"`
	ListStatus            string     `json:"list_status"`
	LastValueID           int64      `json:"last_value_id"`
	SyncedAt              *time.Time `json:"synced_at,omitempty"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
}

type TaskFilter struct {
	ShopID  int64
	Status  string
	BaseSKU string
	Limit   int
	Offset  int
}

type DictQuery struct {
	ShopID      int64
	CategoryID  int64
	TypeID      int64
	AttributeID int64
	Language    string
	Keyword     string
	Limit       int
	Offset      int
}

type ProductFilter struct {
	ShopID           int64
	OfferID          string
	BaseSKU          string
	PublishStatus    string
	CategoryID       int64
	TypeID           int64
	ValidationStatus string
	TextReviewStatus string
	Limit            int
	Offset           int
}
