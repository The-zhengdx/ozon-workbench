package data

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/data/ent"
	"ozon-workbench/internal/data/ent/predicate"
	"ozon-workbench/internal/data/ent/taskevent"
	"ozon-workbench/internal/data/ent/workbenchtask"
)

type taskRepo struct {
	db *ent.Client
}

func NewTaskRepo(data *Data) biz.TaskRepo {
	return &taskRepo{db: data.Client}
}

func (r *taskRepo) Create(ctx context.Context, task *biz.Task, events []biz.TaskEvent) (*biz.Task, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := createTask(tx.WorkbenchTask.Create(), task).Save(ctx)
	if err != nil {
		return nil, err
	}
	if err := insertEvents(ctx, tx, row.ID, row.ShopID, 1, events); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, row.ShopID, row.ID)
}

func (r *taskRepo) Get(ctx context.Context, shopID, id int) (*biz.Task, error) {
	row, err := r.db.WorkbenchTask.Query().Where(
		workbenchtask.IDEQ(id),
		workbenchtask.ShopIDEQ(shopID),
	).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "任务不存在。"}
		}
		return nil, err
	}
	return toBizTask(row)
}

func (r *taskRepo) GetByID(ctx context.Context, id int) (*biz.Task, error) {
	row, err := r.db.WorkbenchTask.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "任务不存在。"}
		}
		return nil, err
	}
	return toBizTask(row)
}

func (r *taskRepo) List(ctx context.Context, shopID int, status string, page, pageSize int) ([]*biz.Task, int, error) {
	preds := []predicate.WorkbenchTask{workbenchtask.ShopIDEQ(shopID)}
	if status != "" {
		preds = append(preds, workbenchtask.StatusEQ(status))
	}
	total, err := r.db.WorkbenchTask.Query().Where(preds...).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.WorkbenchTask.Query().Where(preds...).
		Order(workbenchtask.ByID(sql.OrderDesc())).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*biz.Task, 0, len(rows))
	for _, row := range rows {
		task, err := toBizTask(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, task)
	}
	return out, total, nil
}

func (r *taskRepo) ListRecoverable(ctx context.Context) ([]*biz.Task, error) {
	rows, err := r.db.WorkbenchTask.Query().Where(
		workbenchtask.Or(
			workbenchtask.StatusEQ(biz.StatusRunning),
			workbenchtask.And(
				workbenchtask.StatusEQ(biz.StatusPendingText),
				workbenchtask.AutoTextApproveEQ(true),
				workbenchtask.TextApprovedEQ(false),
			),
			workbenchtask.And(
				workbenchtask.StatusEQ(biz.StatusPendingVisual),
				workbenchtask.AutoVisualApproveEQ(true),
				workbenchtask.VisualApprovedEQ(false),
			),
		),
	).Order(workbenchtask.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*biz.Task, 0, len(rows))
	for _, row := range rows {
		task, err := toBizTask(row)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

func (r *taskRepo) Save(ctx context.Context, expectedRevision int64, task *biz.Task, events []biz.TaskEvent) (*biz.Task, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := updateTask(tx.WorkbenchTask.Update().Where(
		workbenchtask.IDEQ(task.ID),
		workbenchtask.ShopIDEQ(task.ShopID),
		workbenchtask.RevisionEQ(expectedRevision),
	), task).
		SetRevision(expectedRevision + 1).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, &biz.Error{Status: 409, Reason: "REVISION_CONFLICT", Message: "任务已被其他操作更新，请刷新后再试。"}
	}
	seq, err := nextSeq(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}
	if err := insertEvents(ctx, tx, task.ID, task.ShopID, seq, events); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, task.ShopID, task.ID)
}

func (r *taskRepo) ListEvents(ctx context.Context, taskID int, query biz.EventQuery) ([]biz.TaskEvent, bool, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	q := r.db.TaskEvent.Query().Where(taskevent.TaskIDEQ(taskID))
	if query.AfterSeq > 0 {
		q = q.Where(taskevent.SeqGT(query.AfterSeq))
	}
	if query.BeforeSeq > 0 {
		q = q.Where(taskevent.SeqLT(query.BeforeSeq))
	}
	if query.BeforeSeq > 0 && query.AfterSeq == 0 {
		q = q.Order(taskevent.BySeq(sql.OrderDesc()))
	} else {
		q = q.Order(taskevent.BySeq())
	}
	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if query.BeforeSeq > 0 && query.AfterSeq == 0 {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	out := make([]biz.TaskEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizEvent(row))
	}
	return out, hasMore, nil
}

func nextSeq(ctx context.Context, tx *ent.Tx, taskID int) (int, error) {
	row, err := tx.TaskEvent.Query().Where(taskevent.TaskIDEQ(taskID)).Order(taskevent.BySeq(sql.OrderDesc())).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 1, nil
		}
		return 0, err
	}
	return row.Seq + 1, nil
}

func insertEvents(ctx context.Context, tx *ent.Tx, taskID, shopID, seq int, events []biz.TaskEvent) error {
	for i, event := range events {
		if _, err := tx.TaskEvent.Create().
			SetTaskID(taskID).
			SetShopID(shopID).
			SetSessionID(cut(event.SessionID, 128)).
			SetSeq(seq + i).
			SetRole(cut(defaultString(event.Role, biz.RoleSystem), 32)).
			SetStage(cut(event.Stage, 64)).
			SetContent(event.Content).
			SetToolCallID(cut(event.ToolCallID, 128)).
			SetResourceRef(cut(event.ResourceRef, 500)).
			Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func createTask(b *ent.WorkbenchTaskCreate, task *biz.Task) *ent.WorkbenchTaskCreate {
	return b.
		SetShopID(task.ShopID).
		SetCategoryNodeID(task.CategoryNodeID).
		SetDescriptionCategoryID(task.DescriptionCategoryID).
		SetTypeID(task.TypeID).
		SetLanguage(defaultString(task.Language, "ZH_HANS")).
		SetTypeName(cut(task.TypeName, 255)).
		SetTypePath(orStrings(task.TypePath)).
		SetMasterSku(task.MasterSKU).
		SetSupplierURL(cut(task.SupplierURL, 500)).
		SetZipFilename(cut(task.ZipFilename, 255)).
		SetZipLocalPath(cut(task.ZipLocalPath, 500)).
		SetAutoTextApprove(task.AutoTextApprove).
		SetAutoVisualApprove(task.AutoVisualApprove).
		SetTextApproved(task.TextApproved).
		SetTextApprovedAuto(task.TextApprovedAuto).
		SetVisualApproved(task.VisualApproved).
		SetVisualApprovedAuto(task.VisualApprovedAuto).
		SetStatus(defaultString(task.Status, biz.StatusRunning)).
		SetPhase(defaultString(task.Phase, biz.PhaseExtract)).
		SetCodexSessionID(cut(task.CodexSessionID, 128)).
		SetRevision(orInt64(task.Revision, 1)).
		SetMissingFields(jsonText(task.MissingFields, "[]")).
		SetInputValues(jsonText(task.InputValues, "{}")).
		SetSubSkus(jsonText(task.SubSKUs, "[]")).
		SetTextDraft(rawText(task.TextDraft, "{}")).
		SetVisualPlan(rawText(task.VisualPlan, "{}")).
		SetAttributesJSON(rawText(task.Attributes, "[]")).
		SetPromptTemplate(task.PromptTemplate).
		SetRenderedPrompt(task.RenderedPrompt).
		SetErrorMessage(cut(task.ErrorMessage, 4000))
}

func updateTask(b *ent.WorkbenchTaskUpdate, task *biz.Task) *ent.WorkbenchTaskUpdate {
	return b.
		SetCategoryNodeID(task.CategoryNodeID).
		SetDescriptionCategoryID(task.DescriptionCategoryID).
		SetTypeID(task.TypeID).
		SetLanguage(defaultString(task.Language, "ZH_HANS")).
		SetTypeName(cut(task.TypeName, 255)).
		SetTypePath(orStrings(task.TypePath)).
		SetMasterSku(task.MasterSKU).
		SetSupplierURL(cut(task.SupplierURL, 500)).
		SetZipFilename(cut(task.ZipFilename, 255)).
		SetZipLocalPath(cut(task.ZipLocalPath, 500)).
		SetAutoTextApprove(task.AutoTextApprove).
		SetAutoVisualApprove(task.AutoVisualApprove).
		SetTextApproved(task.TextApproved).
		SetTextApprovedAuto(task.TextApprovedAuto).
		SetVisualApproved(task.VisualApproved).
		SetVisualApprovedAuto(task.VisualApprovedAuto).
		SetStatus(defaultString(task.Status, biz.StatusRunning)).
		SetPhase(defaultString(task.Phase, biz.PhaseExtract)).
		SetCodexSessionID(cut(task.CodexSessionID, 128)).
		SetMissingFields(jsonText(task.MissingFields, "[]")).
		SetInputValues(jsonText(task.InputValues, "{}")).
		SetSubSkus(jsonText(task.SubSKUs, "[]")).
		SetTextDraft(rawText(task.TextDraft, "{}")).
		SetVisualPlan(rawText(task.VisualPlan, "{}")).
		SetAttributesJSON(rawText(task.Attributes, "[]")).
		SetPromptTemplate(task.PromptTemplate).
		SetRenderedPrompt(task.RenderedPrompt).
		SetErrorMessage(cut(task.ErrorMessage, 4000))
}

func toBizTask(row *ent.WorkbenchTask) (*biz.Task, error) {
	var missing []biz.MissingField
	if err := json.Unmarshal([]byte(emptyJSON(row.MissingFields, "[]")), &missing); err != nil {
		return nil, err
	}
	var inputs map[string]string
	if err := json.Unmarshal([]byte(emptyJSON(row.InputValues, "{}")), &inputs); err != nil {
		return nil, err
	}
	var skus []biz.SubSKU
	if err := json.Unmarshal([]byte(emptyJSON(row.SubSkus, "[]")), &skus); err != nil {
		return nil, err
	}
	path := row.TypePath
	if path == nil {
		path = []string{}
	}
	return &biz.Task{
		ID: row.ID, ShopID: row.ShopID, CategoryNodeID: row.CategoryNodeID,
		DescriptionCategoryID: row.DescriptionCategoryID, TypeID: row.TypeID, Language: row.Language,
		TypeName: row.TypeName, TypePath: path, MasterSKU: row.MasterSku, SupplierURL: row.SupplierURL,
		ZipFilename: row.ZipFilename, ZipLocalPath: row.ZipLocalPath,
		AutoTextApprove: row.AutoTextApprove, AutoVisualApprove: row.AutoVisualApprove,
		TextApproved: row.TextApproved, TextApprovedAuto: row.TextApprovedAuto,
		VisualApproved: row.VisualApproved, VisualApprovedAuto: row.VisualApprovedAuto,
		Status: row.Status, Phase: row.Phase, CodexSessionID: row.CodexSessionID, Revision: row.Revision,
		MissingFields: missing, InputValues: inputs, SubSKUs: skus,
		TextDraft:      json.RawMessage(emptyJSON(row.TextDraft, "{}")),
		VisualPlan:     json.RawMessage(emptyJSON(row.VisualPlan, "{}")),
		Attributes:     json.RawMessage(emptyJSON(row.AttributesJSON, "[]")),
		PromptTemplate: row.PromptTemplate, RenderedPrompt: row.RenderedPrompt, ErrorMessage: row.ErrorMessage,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func toBizEvent(row *ent.TaskEvent) biz.TaskEvent {
	return biz.TaskEvent{
		ID: row.ID, TaskID: row.TaskID, ShopID: row.ShopID, SessionID: row.SessionID, Seq: row.Seq,
		Role: row.Role, Stage: row.Stage, Content: row.Content, ToolCallID: row.ToolCallID,
		ResourceRef: row.ResourceRef, CreatedAt: row.CreatedAt,
	}
}

func jsonText(v any, fallback string) string {
	if v == nil {
		return fallback
	}
	raw, err := json.Marshal(v)
	if err != nil || string(raw) == "null" {
		return fallback
	}
	return string(raw)
}

func rawText(raw json.RawMessage, fallback string) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return fallback
	}
	return text
}

func emptyJSON(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func orStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func orInt64(v, fallback int64) int64 {
	if v == 0 {
		return fallback
	}
	return v
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func cut(v string, max int) string {
	runes := []rune(v)
	if len(runes) <= max {
		return v
	}
	return string(runes[:max])
}
