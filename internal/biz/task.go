package biz

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"

	"ozon-workbench/internal/media"
)

const (
	RoleUser       = "user"
	RoleAssistant  = "assistant"
	RoleToolCall   = "tool_call"
	RoleToolResult = "tool_result"
	RoleSystem     = "system"
	RolePrompt     = "prompt"
	RoleReview     = "review"

	StatusRunning       = "RUNNING"
	StatusNeedInput     = "NEED_INPUT"
	StatusPendingText   = "PENDING_TEXT_REVIEW"
	StatusPendingVisual = "PENDING_VISUAL_REVIEW"
	StatusCompleted     = "COMPLETED"
	StatusFailed        = "FAILED"

	PhaseExtract      = "extract"
	PhaseAwaitInput   = "await_input"
	PhaseResume       = "resume"
	PhaseTextReview   = "text_review"
	PhaseVisual       = "visual"
	PhaseVisualReview = "visual_review"
	PhaseDone         = "done"
	PhaseFailed       = "failed"
)

// Task is one material-processing job. It is bound to the shop selected at creation.
type Task struct {
	ID                    int
	ShopID                int
	CategoryNodeID        int
	DescriptionCategoryID int64
	TypeID                int64
	Language              string
	TypeName              string
	TypePath              []string
	MasterSKU             string
	SupplierURL           string
	ZipFilename           string
	ZipLocalPath          string
	AutoTextApprove       bool
	AutoVisualApprove     bool
	TextApproved          bool
	TextApprovedAuto      bool
	VisualApproved        bool
	VisualApprovedAuto    bool
	Status                string
	Phase                 string
	CodexSessionID        string
	Revision              int64
	MissingFields         []MissingField
	InputValues           map[string]string
	SubSKUs               []SubSKU
	TextDraft             json.RawMessage
	VisualPlan            json.RawMessage
	Attributes            json.RawMessage
	PromptTemplate        string
	RenderedPrompt        string
	ErrorMessage          string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// TaskEvent is one append-only timeline row. Seq is unique per task.
type TaskEvent struct {
	ID          int
	TaskID      int
	ShopID      int
	SessionID   string
	Seq         int
	Role        string
	Stage       string
	Content     string
	ToolCallID  string
	ResourceRef string
	CreatedAt   time.Time
}

// TaskView is the list and mutation payload. Prompts stay on the detail.
type TaskView struct {
	ID                    int             `json:"id"`
	ShopID                int             `json:"shop_id"`
	CategoryID            int             `json:"category_id"`
	DescriptionCategoryID int64           `json:"description_category_id"`
	TypeID                int64           `json:"type_id"`
	Language              string          `json:"language"`
	TypeName              string          `json:"type_name"`
	TypePath              []string        `json:"type_path"`
	MasterSKU             string          `json:"master_sku"`
	SupplierURL           string          `json:"supplier_url"`
	ZipFilename           string          `json:"zip_filename"`
	ZipLocalPath          string          `json:"zip_local_path"`
	AutoTextApprove       bool            `json:"auto_text_approve"`
	AutoVisualApprove     bool            `json:"auto_visual_approve"`
	TextApproved          bool            `json:"text_approved"`
	TextApprovedAuto      bool            `json:"text_approved_auto"`
	VisualApproved        bool            `json:"visual_approved"`
	VisualApprovedAuto    bool            `json:"visual_approved_auto"`
	Status                string          `json:"status"`
	CodexSessionID        string          `json:"codex_session_id"`
	Revision              int64           `json:"revision"`
	MissingFields         []MissingField  `json:"missing_fields"`
	SubSKUs               []SubSKU        `json:"sub_skus"`
	TextDraft             json.RawMessage `json:"text_draft"`
	VisualPlan            json.RawMessage `json:"visual_plan"`
	ErrorMessage          string          `json:"error_message"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

// TaskDetail adds the frozen prompt and attribute contract.
type TaskDetail struct {
	TaskView
	PromptTemplate string          `json:"prompt_template"`
	RenderedPrompt string          `json:"rendered_prompt"`
	Attributes     json.RawMessage `json:"attributes"`
}

// TaskPage is a shop-scoped task list.
type TaskPage struct {
	Tasks    []TaskView `json:"tasks"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// TaskEventView is one timeline row returned to the client.
type TaskEventView struct {
	ID          int       `json:"id"`
	TaskID      int       `json:"task_id"`
	SessionID   string    `json:"session_id"`
	Seq         int       `json:"seq"`
	Role        string    `json:"role"`
	Stage       string    `json:"stage"`
	Content     string    `json:"content"`
	ToolCallID  string    `json:"tool_call_id"`
	ResourceRef string    `json:"resource_ref"`
	CreatedAt   time.Time `json:"created_at"`
}

// EventPage is a slice of the timeline. HasMore is true when older or newer rows remain.
type EventPage struct {
	Events  []TaskEventView `json:"events"`
	HasMore bool            `json:"has_more"`
}

// EventQuery pages the timeline. AfterSeq reads forward from a cursor.
// BeforeSeq reads the page that ends just before that sequence, back toward the first event.
type EventQuery struct {
	AfterSeq  int
	BeforeSeq int
	Limit     int
}

// TaskRepo is the persistence port. Save is compare-and-swap on revision.
type TaskRepo interface {
	Create(ctx context.Context, task *Task, events []TaskEvent) (*Task, error)
	Get(ctx context.Context, shopID, id int) (*Task, error)
	GetByID(ctx context.Context, id int) (*Task, error)
	List(ctx context.Context, shopID int, status string, page, pageSize int) ([]*Task, int, error)
	ListRecoverable(ctx context.Context) ([]*Task, error)
	Save(ctx context.Context, expectedRevision int64, task *Task, events []TaskEvent) (*Task, error)
	ListEvents(ctx context.Context, taskID int, query EventQuery) ([]TaskEvent, bool, error)
}

// TaskCatalog is the slice of the category port the workbench needs.
type TaskCatalog interface {
	ListNodes(ctx context.Context, language string) ([]FlatNode, error)
	GetPrompt(ctx context.Context, categoryID, typeID int64) (*Prompt, error)
	LatestAttributes(ctx context.Context, categoryID, typeID int64, language string) (*AttrSnapshot, error)
}

// Scheduler runs Codex turns outside the HTTP request.
type Scheduler interface {
	Go(func())
}

// TaskRuntime is the media and timeout configuration for the workbench.
type TaskRuntime struct {
	MediaRoot       string
	MaxZipBytes     int64
	MaxUncompressed int64
	MaxFiles        int
	CodexTimeout    time.Duration
}

// CreateInput is a validated upload plus the shop from X-Shop-Id.
type CreateInput struct {
	ShopID                int
	MasterSKU             string
	SupplierURL           string
	DescriptionCategoryID int64
	TypeID                int64
	Language              string
	AutoText              bool
	AutoVisual            bool
	ZipPath               string
	ZipFilename           string
}

// PolicyDecision says which waiting review a switch change may pass exactly once.
type PolicyDecision struct {
	AdvanceText   bool
	AdvanceVisual bool
}

// DecidePolicy advances only the stage that is waiting, and only when its switch turns on.
// A future stage is not passed here; it reads the saved switch when that stage is reached.
func DecidePolicy(status string, textApproved, visualApproved, prevText, prevVisual, nextText, nextVisual bool) PolicyDecision {
	if status == StatusPendingText && !textApproved && nextText && !prevText {
		return PolicyDecision{AdvanceText: true}
	}
	if status == StatusPendingVisual && !visualApproved && nextVisual && !prevVisual {
		return PolicyDecision{AdvanceVisual: true}
	}
	return PolicyDecision{}
}

// TaskUsecase owns uploads, the review state machine and Codex turns.
type TaskUsecase struct {
	repo    TaskRepo
	shops   ShopRepo
	catalog TaskCatalog
	runner  CodexRunner
	sched   Scheduler
	rt      TaskRuntime
	log     *log.Helper
	mu      sync.Mutex
	locks   map[int]*sync.Mutex
	sink    CompletedTaskSink
}

// CompletedTaskSink is notified after a material task reaches COMPLETED.
// Listing creation is optional so the workbench still runs without the listing module.
type CompletedTaskSink interface {
	OnTaskCompleted(ctx context.Context, task *Task)
}

// SetListingSink attaches the module that turns an approved plan into sellable SKUs.
func (uc *TaskUsecase) SetListingSink(sink CompletedTaskSink) {
	if uc != nil {
		uc.sink = sink
	}
}

func (uc *TaskUsecase) noteCompleted(ctx context.Context, task *Task) {
	if task == nil || task.Status != StatusCompleted {
		return
	}
	writeApprovedPlan(task)
	if uc.sink != nil {
		uc.sink.OnTaskCompleted(ctx, task)
	}
}

func NewTaskUsecase(repo TaskRepo, shops ShopRepo, catalog TaskCatalog, runner CodexRunner, sched Scheduler, rt TaskRuntime, logger log.Logger) (*TaskUsecase, error) {
	if rt.MediaRoot == "" {
		rt.MediaRoot = "data/media"
	}
	if rt.MaxZipBytes <= 0 {
		rt.MaxZipBytes = 200 << 20
	}
	if rt.MaxUncompressed <= 0 {
		rt.MaxUncompressed = 500 << 20
	}
	if rt.MaxFiles <= 0 {
		rt.MaxFiles = 4000
	}
	if rt.CodexTimeout <= 0 {
		rt.CodexTimeout = 15 * time.Minute
	}
	if err := os.MkdirAll(filepath.Join(rt.MediaRoot, "incoming"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(rt.MediaRoot, "tasks"), 0o755); err != nil {
		return nil, err
	}
	return &TaskUsecase{
		repo: repo, shops: shops, catalog: catalog, runner: runner, sched: sched, rt: rt,
		log: log.NewHelper(logger), locks: map[int]*sync.Mutex{},
	}, nil
}

// DiscardUpload removes a temp zip that never became a task.
func (uc *TaskUsecase) DiscardUpload(path string) error {
	if err := uc.insideRoot(path); err != nil {
		return err
	}
	return os.Remove(path)
}

// SaveUpload streams a zip to the media root. The body is never buffered as a whole.
func (uc *TaskUsecase) SaveUpload(r io.Reader, filename string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return "", bad("ZIP_TYPE", "请上传 .zip 素材包。", map[string]string{"file": "请上传 .zip 素材包。"})
	}
	dir := filepath.Join(uc.rt.MediaRoot, "incoming")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, uuid.NewString()+".zip")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	n, err := io.Copy(file, io.LimitReader(r, uc.rt.MaxZipBytes+1))
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if n == 0 {
		_ = os.Remove(path)
		return "", bad("ZIP_EMPTY", "请上传 1688 素材压缩包。", map[string]string{"file": "请上传 1688 素材压缩包。"})
	}
	if n > uc.rt.MaxZipBytes {
		_ = os.Remove(path)
		return "", bad("ZIP_TOO_LARGE", "压缩包超过大小限制。", map[string]string{"file": "压缩包超过大小限制。"})
	}
	return path, nil
}

// Create binds the task to the shop, extracts the zip, deletes it, and starts Codex.
func (uc *TaskUsecase) Create(ctx context.Context, in CreateInput) (*TaskDetail, error) {
	zipPath := in.ZipPath
	consumed := false
	defer func() {
		if !consumed && zipPath != "" {
			_ = os.Remove(zipPath)
		}
	}()
	if strings.TrimSpace(zipPath) == "" {
		return nil, bad("ZIP_REQUIRED", "请上传 1688 素材压缩包。", map[string]string{"file": "请上传 1688 素材压缩包。"})
	}
	if err := uc.insideRoot(zipPath); err != nil {
		return nil, err
	}
	sku, err := normalizeSKU(in.MasterSKU)
	if err != nil {
		return nil, err
	}
	supplier, err := normalizeSupplierURL(in.SupplierURL)
	if err != nil {
		return nil, err
	}
	language := strings.TrimSpace(in.Language)
	if language == "" {
		language = "ZH_HANS"
	}
	shop, err := uc.shops.Get(ctx, in.ShopID)
	if err != nil {
		return nil, err
	}
	if shop.Archived {
		return nil, bad("SHOP_ARCHIVED", "店铺已移除，请先恢复。", nil)
	}
	leaf, err := findLeaf(ctx, uc.catalog, language, in.DescriptionCategoryID, in.TypeID)
	if err != nil {
		return nil, err
	}
	prompt, err := uc.catalog.GetPrompt(ctx, leaf.DescriptionCategoryID, leaf.TypeID)
	if err != nil {
		return nil, err
	}
	if prompt == nil || strings.TrimSpace(prompt.CodexPrompt) == "" {
		return nil, bad("PROMPT_EMPTY", "这个末级类型还没有保存 Codex 提示词。", map[string]string{
			"type_id": "请先为这个末级类型保存 Codex 提示词。",
		})
	}
	attrs, attrRaw, err := loadAttributeContract(ctx, uc.catalog, leaf.DescriptionCategoryID, leaf.TypeID, language)
	if err != nil {
		return nil, err
	}
	rendered, err := RenderPrompt(prompt.CodexPrompt, sku, attrs)
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(uc.rt.MediaRoot, "tasks", uuid.NewString())
	if len(dest) > 500 {
		return nil, bad("PATH_TOO_LONG", "媒体目录路径过长，请缩短 media.root。", nil)
	}
	task := &Task{
		ShopID:                shop.ID,
		CategoryNodeID:        leaf.LocalID,
		DescriptionCategoryID: leaf.DescriptionCategoryID,
		TypeID:                leaf.TypeID,
		Language:              language,
		TypeName:              leaf.Name,
		TypePath:              append([]string(nil), leaf.Path...),
		MasterSKU:             sku,
		SupplierURL:           supplier,
		ZipFilename:           cleanFilename(in.ZipFilename),
		AutoTextApprove:       in.AutoText,
		AutoVisualApprove:     in.AutoVisual,
		Status:                StatusRunning,
		Phase:                 PhaseExtract,
		Revision:              1,
		Attributes:            attrRaw,
		PromptTemplate:        prompt.CodexPrompt,
		RenderedPrompt:        rendered,
	}
	unpackErr := media.Unpack(zipPath, dest, media.Limits{MaxBytes: uc.rt.MaxUncompressed, MaxFiles: uc.rt.MaxFiles})
	consumed = true
	if unpackErr != nil {
		task.Status = StatusFailed
		task.Phase = PhaseFailed
		task.ErrorMessage = truncateRunes(unpackErr.Error(), 1800)
		created, err := uc.repo.Create(ctx, task, []TaskEvent{{
			Role: RoleSystem, Stage: "错误", Content: "解压失败：" + task.ErrorMessage,
		}})
		if err != nil {
			return nil, err
		}
		detail := created.Detail()
		return &detail, nil
	}
	task.ZipLocalPath = dest
	created, err := uc.repo.Create(ctx, task, initialEvents(task))
	if err != nil {
		_ = os.RemoveAll(dest)
		return nil, err
	}
	uc.schedule(func() { uc.runExtract(context.Background(), created.ShopID, created.ID) })
	latest, err := uc.repo.Get(ctx, created.ShopID, created.ID)
	if err != nil {
		detail := created.Detail()
		return &detail, nil
	}
	detail := latest.Detail()
	return &detail, nil
}

func (uc *TaskUsecase) List(ctx context.Context, shopID int, status string, page, pageSize int) (*TaskPage, error) {
	if _, err := uc.requireShop(ctx, shopID); err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	if status != "" && !knownStatus(status) {
		return nil, bad("BAD_STATUS", "未知的任务状态。", nil)
	}
	page, pageSize = pageBounds(page, pageSize)
	rows, total, err := uc.repo.List(ctx, shopID, status, page, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Summary())
	}
	return &TaskPage{Tasks: out, Total: total, Page: page, PageSize: pageSize}, nil
}

func (uc *TaskUsecase) Get(ctx context.Context, shopID, id int) (*TaskDetail, error) {
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	detail := task.Detail()
	return &detail, nil
}

func (uc *TaskUsecase) Events(ctx context.Context, shopID, id int, query EventQuery) (*EventPage, error) {
	if _, err := uc.repo.Get(ctx, shopID, id); err != nil {
		return nil, err
	}
	query.Limit = eventLimit(query.Limit)
	rows, hasMore, err := uc.repo.ListEvents(ctx, id, query)
	if err != nil {
		return nil, err
	}
	out := make([]TaskEventView, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.View())
	}
	return &EventPage{Events: out, HasMore: hasMore}, nil
}

// UpdatePolicy saves the two task-level switches and, when the waiting stage's
// switch turns on, passes that stage once under the revision check.
func (uc *TaskUsecase) UpdatePolicy(ctx context.Context, shopID, id int, revision int64, autoText, autoVisual bool) (*TaskView, error) {
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if task.Revision != revision {
		return nil, revisionConflict()
	}
	if task.AutoTextApprove == autoText && task.AutoVisualApprove == autoVisual {
		view := task.Summary()
		return &view, nil
	}
	decision := DecidePolicy(task.Status, task.TextApproved, task.VisualApproved, task.AutoTextApprove, task.AutoVisualApprove, autoText, autoVisual)
	next := cloneTask(task)
	var events []TaskEvent
	if next.AutoTextApprove != autoText {
		next.AutoTextApprove = autoText
		events = append(events, policyEvent("文案", autoText))
	}
	if next.AutoVisualApprove != autoVisual {
		next.AutoVisualApprove = autoVisual
		events = append(events, policyEvent("视觉方案", autoVisual))
	}
	scheduleVisual := false
	if decision.AdvanceText {
		events = append(events, markTextPassed(next, true)...)
		scheduleVisual = true
	} else if decision.AdvanceVisual {
		events = append(events, markVisualPassed(next, true)...)
	}
	saved, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events))
	if err != nil {
		return nil, err
	}
	if decision.AdvanceVisual {
		uc.noteCompleted(ctx, saved)
	}
	if scheduleVisual {
		uc.schedule(func() { uc.runVisual(context.Background(), saved.ShopID, saved.ID) })
	}
	latest, err := uc.repo.Get(ctx, saved.ShopID, saved.ID)
	if err != nil {
		view := saved.Summary()
		return &view, nil
	}
	view := latest.Summary()
	return &view, nil
}

// Approve passes the current human review. Text and visual share the revision check
// so a second request cannot advance the same stage again.
func (uc *TaskUsecase) Approve(ctx context.Context, shopID, id int, stage string, revision int64) (*TaskView, error) {
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if task.Revision != revision {
		return nil, revisionConflict()
	}
	next := cloneTask(task)
	var events []TaskEvent
	scheduleVisual := false
	switch stage {
	case "text":
		if task.Status != StatusPendingText || task.TextApproved {
			return nil, bad("WRONG_STAGE", "当前不是文案审核环节。", nil)
		}
		events = markTextPassed(next, false)
		scheduleVisual = true
	case "visual":
		if task.Status != StatusPendingVisual || task.VisualApproved {
			return nil, bad("WRONG_STAGE", "当前不是视觉方案审核环节。", nil)
		}
		events = markVisualPassed(next, false)
	default:
		return nil, bad("BAD_STAGE", "审核环节不正确。", nil)
	}
	saved, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events))
	if err != nil {
		return nil, err
	}
	if stage == "visual" {
		uc.noteCompleted(ctx, saved)
	}
	if scheduleVisual {
		uc.schedule(func() { uc.runVisual(context.Background(), saved.ShopID, saved.ID) })
	}
	latest, err := uc.repo.Get(ctx, saved.ShopID, saved.ID)
	if err != nil {
		view := saved.Summary()
		return &view, nil
	}
	view := latest.Summary()
	return &view, nil
}

// SubmitInput resumes the same Codex session after the operator fills NEED_INPUT.
func (uc *TaskUsecase) SubmitInput(ctx context.Context, shopID, id int, revision int64, fields map[string]string) (*TaskView, error) {
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if task.Revision != revision {
		return nil, revisionConflict()
	}
	if task.Status != StatusNeedInput {
		return nil, bad("WRONG_STAGE", "当前任务不在待补充数据。", nil)
	}
	answers, err := collectAnswers(task.MissingFields, fields)
	if err != nil {
		return nil, err
	}
	next := cloneTask(task)
	next.InputValues = answers
	next.MissingFields = nil
	next.Status = StatusRunning
	next.Phase = PhaseResume
	raw, _ := json.MarshalIndent(answers, "", "  ")
	saved, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, []TaskEvent{{
		Role: RoleUser, Stage: "补充", Content: "已补充字段：\n" + string(raw),
	}}))
	if err != nil {
		return nil, err
	}
	uc.schedule(func() { uc.runResume(context.Background(), saved.ShopID, saved.ID) })
	latest, err := uc.repo.Get(ctx, saved.ShopID, saved.ID)
	if err != nil {
		view := saved.Summary()
		return &view, nil
	}
	view := latest.Summary()
	return &view, nil
}

// FollowUp appends a user turn to the same session and asks Codex to continue.
func (uc *TaskUsecase) FollowUp(ctx context.Context, shopID, id int, revision int64, content string) (*TaskView, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, bad("MESSAGE_EMPTY", "请填写追问内容。", map[string]string{"content": "请填写追问内容。"})
	}
	if utf8.RuneCountInString(content) > 8000 {
		return nil, bad("MESSAGE_LONG", "追问内容过长。", nil)
	}
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if task.Revision != revision {
		return nil, revisionConflict()
	}
	if task.Status == StatusRunning {
		return nil, bad("TASK_BUSY", "任务正在运行，请稍后再追问。", nil)
	}
	if task.CodexSessionID == "" {
		return nil, bad("NO_SESSION", "会话尚未建立。", nil)
	}
	next := cloneTask(task)
	saved, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, []TaskEvent{{
		Role: RoleUser, Stage: "追问", Content: content,
	}}))
	if err != nil {
		return nil, err
	}
	uc.schedule(func() { uc.runFollowUp(context.Background(), saved.ShopID, saved.ID, content) })
	latest, err := uc.repo.Get(ctx, saved.ShopID, saved.ID)
	if err != nil {
		view := saved.Summary()
		return &view, nil
	}
	view := latest.Summary()
	return &view, nil
}

// MediaPath resolves a file inside the task's extracted directory.
func (uc *TaskUsecase) MediaPath(ctx context.Context, id int, name string) (string, error) {
	task, err := uc.repo.GetByID(ctx, id)
	if err != nil || task.ZipLocalPath == "" {
		return "", notFound("素材不存在。")
	}
	full, err := media.SafeJoin(task.ZipLocalPath, name)
	if err != nil {
		return "", notFound("素材不存在。")
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return "", notFound("素材不存在。")
	}
	return full, nil
}

// Recover restarts turns that were still running when the process stopped.
func (uc *TaskUsecase) Recover(ctx context.Context) error {
	rows, err := uc.repo.ListRecoverable(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		row := row
		switch {
		case row.Status == StatusRunning && row.Phase == PhaseVisual:
			uc.schedule(func() { uc.runVisual(context.Background(), row.ShopID, row.ID) })
		case row.Status == StatusRunning && row.Phase == PhaseResume:
			uc.schedule(func() { uc.runResume(context.Background(), row.ShopID, row.ID) })
		case row.Status == StatusRunning:
			uc.schedule(func() { uc.runExtract(context.Background(), row.ShopID, row.ID) })
		case row.Status == StatusPendingText && row.AutoTextApprove && !row.TextApproved:
			uc.schedule(func() { uc.recoverText(context.Background(), row.ShopID, row.ID) })
		case row.Status == StatusPendingVisual && row.AutoVisualApprove && !row.VisualApproved:
			uc.schedule(func() { uc.recoverVisual(context.Background(), row.ShopID, row.ID) })
		}
	}
	return nil
}

func (uc *TaskUsecase) recoverText(ctx context.Context, shopID, id int) {
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil || task.Status != StatusPendingText || !task.AutoTextApprove || task.TextApproved {
		return
	}
	next := cloneTask(task)
	events := markTextPassed(next, true)
	if _, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events)); err != nil {
		return
	}
	uc.runVisual(ctx, shopID, id)
}

func (uc *TaskUsecase) recoverVisual(ctx context.Context, shopID, id int) {
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil || task.Status != StatusPendingVisual || !task.AutoVisualApprove || task.VisualApproved {
		return
	}
	next := cloneTask(task)
	events := markVisualPassed(next, true)
	saved, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events))
	if err != nil {
		return
	}
	uc.noteCompleted(ctx, saved)
}

func (t *Task) Summary() TaskView {
	path := t.TypePath
	if path == nil {
		path = []string{}
	}
	fields := t.MissingFields
	if fields == nil {
		fields = []MissingField{}
	}
	skus := t.SubSKUs
	if skus == nil {
		skus = []SubSKU{}
	}
	draft := t.TextDraft
	if len(draft) == 0 {
		draft = json.RawMessage(`{}`)
	}
	plan := t.VisualPlan
	if len(plan) == 0 {
		plan = json.RawMessage(`{}`)
	}
	return TaskView{
		ID: t.ID, ShopID: t.ShopID, CategoryID: t.CategoryNodeID,
		DescriptionCategoryID: t.DescriptionCategoryID, TypeID: t.TypeID, Language: t.Language,
		TypeName: t.TypeName, TypePath: path, MasterSKU: t.MasterSKU, SupplierURL: t.SupplierURL,
		ZipFilename: t.ZipFilename, ZipLocalPath: t.ZipLocalPath,
		AutoTextApprove: t.AutoTextApprove, AutoVisualApprove: t.AutoVisualApprove,
		TextApproved: t.TextApproved, TextApprovedAuto: t.TextApprovedAuto,
		VisualApproved: t.VisualApproved, VisualApprovedAuto: t.VisualApprovedAuto,
		Status: t.Status, CodexSessionID: t.CodexSessionID, Revision: t.Revision,
		MissingFields: fields, SubSKUs: skus, TextDraft: draft, VisualPlan: plan,
		ErrorMessage: t.ErrorMessage, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func (t *Task) Detail() TaskDetail {
	attrs := t.Attributes
	if len(attrs) == 0 {
		attrs = json.RawMessage(`[]`)
	}
	return TaskDetail{TaskView: t.Summary(), PromptTemplate: t.PromptTemplate, RenderedPrompt: t.RenderedPrompt, Attributes: attrs}
}

func (e TaskEvent) View() TaskEventView {
	return TaskEventView{
		ID: e.ID, TaskID: e.TaskID, SessionID: e.SessionID, Seq: e.Seq, Role: e.Role,
		Stage: e.Stage, Content: e.Content, ToolCallID: e.ToolCallID, ResourceRef: e.ResourceRef, CreatedAt: e.CreatedAt,
	}
}

func markTextPassed(task *Task, auto bool) []TaskEvent {
	task.TextApproved = true
	task.TextApprovedAuto = auto
	task.Status = StatusRunning
	task.Phase = PhaseVisual
	return []TaskEvent{reviewEvent("文案", auto)}
}

func markVisualPassed(task *Task, auto bool) []TaskEvent {
	task.VisualApproved = true
	task.VisualApprovedAuto = auto
	task.Status = StatusCompleted
	task.Phase = PhaseDone
	return []TaskEvent{
		reviewEvent("视觉方案", auto),
		{Role: RoleSystem, Stage: "完成", Content: "文案和视觉方案均已确认，加工完成。"},
	}
}

func reviewEvent(kind string, auto bool) TaskEvent {
	how := "已人工通过"
	if auto {
		how = "按本任务设置自动通过"
	}
	return TaskEvent{Role: RoleReview, Stage: "审核", Content: kind + "审核" + how + "。"}
}

func policyEvent(kind string, on bool) TaskEvent {
	mode := "人工审核"
	if on {
		mode = "自动通过"
	}
	return TaskEvent{Role: RoleSystem, Stage: "设置", Content: kind + "审核已切换为" + mode + "。"}
}

func initialEvents(task *Task) []TaskEvent {
	link := task.SupplierURL
	if link == "" {
		link = "未提供"
	}
	path := strings.Join(task.TypePath, " / ")
	if path == "" {
		path = task.TypeName
	}
	body := "请处理素材包 " + task.ZipFilename + "。\n主 SKU：" + task.MasterSKU + "\n目标类型：" + path + "\n供应商链接：" + link
	return []TaskEvent{
		{Role: RoleSystem, Stage: "创建", Content: "创建任务，绑定店铺与本次会话。"},
		{Role: RoleUser, Stage: "输入", Content: body},
		{Role: RolePrompt, Stage: "提示词", Content: task.RenderedPrompt},
	}
}

func revisionConflict() *Error {
	return &Error{Status: 409, Reason: "REVISION_CONFLICT", Message: "任务已被其他操作更新，请刷新后再试。"}
}

func isConflict(err error) bool {
	be, ok := isBiz(err)
	return ok && be != nil && be.Reason == "REVISION_CONFLICT"
}

func knownStatus(status string) bool {
	switch status {
	case StatusRunning, StatusNeedInput, StatusPendingText, StatusPendingVisual, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

func pageBounds(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func eventLimit(n int) int {
	if n <= 0 {
		return 100
	}
	if n > 500 {
		return 500
	}
	return n
}

func normalizeSKU(sku string) (string, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return "", bad("SKU_REQUIRED", "请填写主 SKU。", map[string]string{"master_sku": "请填写主 SKU。"})
	}
	if utf8.RuneCountInString(sku) > 64 {
		return "", bad("SKU_LONG", "主 SKU 不能超过 64 个字符。", map[string]string{"master_sku": "主 SKU 不能超过 64 个字符。"})
	}
	for _, r := range sku {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", bad("SKU_CHAR", "主 SKU 只能包含字母、数字、连字符、下划线和点。", map[string]string{"master_sku": "主 SKU 只能包含字母、数字、连字符、下划线和点。"})
	}
	return sku, nil
}

func normalizeSupplierURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	lower := strings.ToLower(raw)
	if len(raw) > 500 || (!strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://")) || strings.ContainsAny(raw, " \t\r\n") {
		return "", bad("BAD_URL", "请输入完整的 http 或 https 供应商链接。", map[string]string{"supplier_url": "请输入完整的 http 或 https 供应商链接。"})
	}
	rest := raw
	if i := strings.Index(raw, "://"); i >= 0 {
		rest = raw[i+3:]
	}
	if rest == "" || strings.HasPrefix(rest, "/") {
		return "", bad("BAD_URL", "请输入完整的 http 或 https 供应商链接。", map[string]string{"supplier_url": "请输入完整的 http 或 https 供应商链接。"})
	}
	return raw, nil
}

func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "upload.zip"
	}
	if utf8.RuneCountInString(name) > 255 {
		runes := []rune(name)
		name = string(runes[len(runes)-255:])
	}
	return name
}

func findLeaf(ctx context.Context, catalog TaskCatalog, language string, categoryID, typeID int64) (*FlatNode, error) {
	if categoryID <= 0 || typeID <= 0 {
		return nil, bad("BAD_TYPE", "请选择可用的末级类型。", map[string]string{"type_id": "请选择可用的末级类型。"})
	}
	nodes, err := catalog.ListNodes(ctx, language)
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		node := &nodes[i]
		if node.IsLeaf && node.DescriptionCategoryID == categoryID && node.TypeID == typeID {
			if node.Disabled || node.AncestorDisabled {
				return nil, bad("TYPE_DISABLED", "这个末级类型已停用，不能新建任务。", map[string]string{"type_id": "这个末级类型已停用，不能新建任务。"})
			}
			return node, nil
		}
	}
	return nil, notFound("当前语言的类目树中没有这个商品类型。")
}

func loadAttributeContract(ctx context.Context, catalog TaskCatalog, categoryID, typeID int64, language string) ([]map[string]any, json.RawMessage, error) {
	snap, err := catalog.LatestAttributes(ctx, categoryID, typeID, language)
	if err != nil {
		return nil, nil, err
	}
	if snap == nil || snap.Attributes == nil {
		return []map[string]any{}, json.RawMessage(`[]`), nil
	}
	raw, err := json.Marshal(snap.Attributes)
	if err != nil {
		return nil, nil, err
	}
	return snap.Attributes, raw, nil
}

func collectAnswers(missing []MissingField, fields map[string]string) (map[string]string, error) {
	if fields == nil {
		fields = map[string]string{}
	}
	out := map[string]string{}
	missingKeys := map[string]string{}
	for _, field := range missing {
		value := strings.TrimSpace(fields[field.Key])
		if field.Required && value == "" {
			missingKeys[field.Key] = "请填写" + field.Label
			continue
		}
		if utf8.RuneCountInString(value) > 2000 {
			missingKeys[field.Key] = field.Label + "过长"
			continue
		}
		if value != "" {
			out[field.Key] = value
		}
	}
	if len(missingKeys) > 0 {
		return nil, fieldError(missingKeys)
	}
	return out, nil
}

func (uc *TaskUsecase) requireShop(ctx context.Context, shopID int) (*Shop, error) {
	shop, err := uc.shops.Get(ctx, shopID)
	if err != nil {
		return nil, err
	}
	if shop.Archived {
		return nil, bad("SHOP_ARCHIVED", "店铺已移除，请先恢复。", nil)
	}
	return shop, nil
}

func (uc *TaskUsecase) insideRoot(path string) error {
	root, err := filepath.Abs(uc.rt.MediaRoot)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return bad("BAD_ZIP", "压缩包路径不正确。", nil)
	}
	return nil
}

func (uc *TaskUsecase) schedule(fn func()) {
	if uc.sched == nil {
		go fn()
		return
	}
	uc.sched.Go(fn)
}

func (uc *TaskUsecase) withLock(id int, fn func()) {
	uc.mu.Lock()
	m := uc.locks[id]
	if m == nil {
		m = &sync.Mutex{}
		uc.locks[id] = m
	}
	uc.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	defer func() {
		if rec := recover(); rec != nil {
			uc.log.Errorf("task %d panic: %v", id, rec)
		}
	}()
	fn()
}

func cloneTask(task *Task) *Task {
	raw, err := json.Marshal(task)
	if err != nil {
		cp := *task
		return &cp
	}
	var out Task
	if err := json.Unmarshal(raw, &out); err != nil {
		cp := *task
		return &cp
	}
	return &out
}

func stamp(session string, events []TaskEvent) []TaskEvent {
	for i := range events {
		if events[i].SessionID == "" {
			events[i].SessionID = session
		}
		events[i].Content = Redact(events[i].Content)
	}
	return events
}
