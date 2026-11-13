package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/store"
)

type Upload struct {
	Name string
	Data []byte
}

type NewTask struct {
	ShopID                  int64
	CategoryID              int64
	TypeID                  int64
	BaseSKU                 string
	OriginLink              string
	CopyAutoApprove         *bool
	VisualPlanAutoApprove   *bool
	VisualResultAutoApprove *bool
	Files                   []Upload
	IdemKey                 string
}

type TaskView struct {
	domain.Task
	QueuePosition int  `json:"queue_position,omitempty"`
	HoldsSlot     bool `json:"holds_slot"`
}

func (e *Engine) CreateTask(ctx context.Context, in NewTask) (TaskView, error) {
	if strings.TrimSpace(in.BaseSKU) == "" || in.CategoryID <= 0 || in.TypeID <= 0 {
		return TaskView{}, fail(400, "TASK_FIELDS", "需要主 SKU、类目和类型", nil)
	}
	shop, err := e.shopEnabled(ctx, in.ShopID)
	if err != nil {
		return TaskView{}, err
	}
	if in.IdemKey != "" {
		var raw json.RawMessage
		var ok bool
		err := e.DB.InTx(ctx, func(tx store.Tx) error {
			var err error
			raw, ok, err = tx.GetIdem(ctx, "task", in.IdemKey)
			return err
		})
		if err != nil {
			return TaskView{}, err
		}
		if ok {
			var view TaskView
			if err := json.Unmarshal(raw, &view); err == nil && view.ID > 0 {
				return view, nil
			}
		}
	}
	var view TaskView
	err = e.DB.InTx(ctx, func(tx store.Tx) error {
		rule, ok, err := tx.GetRule(ctx, in.CategoryID, in.TypeID)
		if err != nil {
			return err
		}
		generic := strings.TrimSpace(configString(ctx, tx, "generic_prompt", ""))
		prompt := ""
		var ruleID *int64
		var version *int
		if ok && rule.Enabled {
			prompt = ComposePrompt(generic, rule.Prompt)
			ruleID = &rule.ID
			version = &rule.Version
		} else if configString(ctx, tx, "prompt_fallback", "block") == "generic" && generic != "" {
			prompt = ComposePrompt(generic, "")
		} else {
			return fail(409, "PROMPT_REQUIRED", "这个类目没有启用的提示词，系统也没有可用的通用提示词", nil)
		}
		prompt = RenderPrompt(prompt, in.BaseSKU)
		reviews := configReviews(ctx, tx)
		copyAuto, planAuto, resultAuto := reviews.Copy, reviews.VisualPlan, reviews.VisualResult
		if in.CopyAutoApprove != nil {
			copyAuto = *in.CopyAutoApprove
		}
		if in.VisualPlanAutoApprove != nil {
			planAuto = *in.VisualPlanAutoApprove
		}
		if in.VisualResultAutoApprove != nil {
			resultAuto = *in.VisualResultAutoApprove
		}
		token, err := randomToken()
		if err != nil {
			return err
		}
		now := e.now()
		task, err := tx.CreateTask(ctx, domain.Task{
			ShopID: shop.ID, DescriptionCategoryID: in.CategoryID, TypeID: in.TypeID,
			BaseSKU: strings.TrimSpace(in.BaseSKU), OriginLink: strings.TrimSpace(in.OriginLink),
			CategoryRuleID: ruleID, CategoryRuleVersion: version, PromptSnapshot: prompt,
			CategoryToken: token, Status: domain.StatusQueued, Stage: "extract",
			CopyAutoApprove: copyAuto, VisualPlanAutoApprove: planAuto, VisualResultAutoApprove: resultAuto,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		view = TaskView{Task: task, HoldsSlot: false}
		if in.IdemKey != "" {
			blob, _ := json.Marshal(view)
			if err := tx.PutIdem(ctx, "task", in.IdemKey, blob, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return TaskView{}, err
	}
	if err := e.writeUploads(view.ID, in.Files); err != nil {
		_ = e.DB.InTx(ctx, func(tx store.Tx) error {
			task, err := tx.GetTask(ctx, view.ID)
			if err != nil {
				return err
			}
			task.Status = domain.StatusFailed
			task.LastError = err.Error()
			now := e.now()
			task.FinishedAt = &now
			task.UpdatedAt = now
			return tx.UpdateTask(ctx, task)
		})
		return TaskView{}, fail(400, "UPLOAD", err.Error(), nil)
	}
	view.QueuePosition = e.queuePosition(ctx, view.ID)
	return view, nil
}

func (e *Engine) ListTasks(ctx context.Context, filter domain.TaskFilter) ([]TaskView, error) {
	var tasks []domain.Task
	var queued []domain.Task
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		tasks, err = tx.ListTasks(ctx, filter)
		if err != nil {
			return err
		}
		queued, err = tx.ListTasks(ctx, domain.TaskFilter{Status: domain.StatusQueued, Limit: 100000})
		return err
	})
	if err != nil {
		return nil, err
	}
	pos := map[int64]int{}
	// ListTasks orders newest first. Queue position follows created_at, id.
	ordered := append([]domain.Task(nil), queued...)
	sortTasksFIFO(ordered)
	for i, task := range ordered {
		pos[task.ID] = i + 1
	}
	out := make([]TaskView, 0, len(tasks))
	for _, task := range tasks {
		task.CategoryToken = ""
		task.PromptSnapshot = ""
		out = append(out, TaskView{Task: task, QueuePosition: pos[task.ID], HoldsSlot: domain.HoldsSlot(task.Status)})
	}
	return out, nil
}

func (e *Engine) GetTask(ctx context.Context, id int64) (TaskView, error) {
	var task domain.Task
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		task, err = tx.GetTask(ctx, id)
		return err
	})
	if err == store.ErrNotFound {
		return TaskView{}, fail(404, "TASK_NOT_FOUND", "加工任务不存在", nil)
	}
	if err != nil {
		return TaskView{}, err
	}
	return TaskView{Task: task, QueuePosition: e.queuePosition(ctx, id), HoldsSlot: domain.HoldsSlot(task.Status)}, nil
}

func (e *Engine) Messages(ctx context.Context, taskID int64) ([]domain.Message, error) {
	var msgs []domain.Message
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		if _, err := tx.GetTask(ctx, taskID); err != nil {
			return err
		}
		var err error
		msgs, err = tx.ListMessages(ctx, taskID)
		return err
	})
	if err == store.ErrNotFound {
		return nil, fail(404, "TASK_NOT_FOUND", "加工任务不存在", nil)
	}
	return msgs, err
}

func (e *Engine) AddMessage(ctx context.Context, taskID int64, content, idem string) (domain.Message, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.Message{}, fail(400, "MESSAGE_EMPTY", "补充内容不能为空", nil)
	}
	if idem != "" {
		var raw json.RawMessage
		var ok bool
		_ = e.DB.InTx(ctx, func(tx store.Tx) error {
			raw, ok, _ = tx.GetIdem(ctx, "message:"+strconv.FormatInt(taskID, 10), idem)
			return nil
		})
		if ok {
			var msg domain.Message
			if json.Unmarshal(raw, &msg) == nil && msg.ID > 0 {
				return msg, nil
			}
		}
	}
	var saved domain.Message
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), taskID)
		if err != nil {
			return err
		}
		if task.Status == domain.StatusCompleted || task.Status == domain.StatusFailed || task.Status == domain.StatusCancelled {
			return fail(409, "TASK_CLOSED", "任务已经结束，不能再追加要求", nil)
		}
		task.RequirementRevision++
		rev := task.RequirementRevision
		task.UpdatedAt = e.now()
		if err := tx.UpdateTask(ctx, task); err != nil {
			return err
		}
		saved, err = tx.CreateMessage(ctx, domain.Message{
			TaskID: taskID, Role: domain.RoleUser, Kind: domain.KindUserRequirement, Content: content,
			RequirementRevision: &rev, DeliveryStatus: domain.DeliveryPending, CreatedAt: e.now(),
		})
		if err != nil {
			return err
		}
		if idem != "" {
			blob, _ := json.Marshal(saved)
			return tx.PutIdem(ctx, "message:"+strconv.FormatInt(taskID, 10), idem, blob, e.now())
		}
		return nil
	})
	if err == store.ErrNotFound {
		return domain.Message{}, fail(404, "TASK_NOT_FOUND", "加工任务不存在", nil)
	}
	return saved, err
}

type ReviewInput struct {
	Kind     string `json:"kind"`
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (e *Engine) Review(ctx context.Context, taskID int64, in ReviewInput) error {
	switch in.Kind {
	case "copy", "visual_plan", "visual_result", "final":
	default:
		return fail(400, "REVIEW_KIND", "审核类型无效", nil)
	}
	if in.Decision != "approve" && in.Decision != "reject" {
		return fail(400, "REVIEW_DECISION", "审核决定只能是 approve 或 reject", nil)
	}
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), taskID)
		if err != nil {
			return err
		}
		approve := in.Decision == "approve"
		switch in.Kind {
		case "copy":
			task.CopyApproved = approve
		case "visual_plan":
			task.VisualPlanApproved = approve
		case "visual_result", "final":
			task.VisualResultApproved = approve
			if in.Kind == "final" {
				task.CopyApproved = approve
				task.VisualPlanApproved = approve
			}
		}
		task.RequirementRevision++
		rev := task.RequirementRevision
		task.UpdatedAt = e.now()
		if err := tx.UpdateTask(ctx, task); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"kind": in.Kind, "decision": in.Decision, "note": in.Note})
		_, err = tx.CreateMessage(ctx, domain.Message{
			TaskID: taskID, Role: domain.RoleUser, Kind: domain.KindReviewDecision,
			Content: in.Kind + ":" + in.Decision + " " + in.Note, Payload: payload,
			RequirementRevision: &rev, DeliveryStatus: domain.DeliveryPending, CreatedAt: e.now(),
		})
		return err
	})
}

func (e *Engine) CancelTask(ctx context.Context, taskID int64) error {
	e.mu.Lock()
	cancel := e.cancels[taskID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), taskID)
		if err != nil {
			return err
		}
		if task.Status == domain.StatusCompleted || task.Status == domain.StatusCancelled || task.Status == domain.StatusFailed {
			return nil
		}
		task.CancelRequested = true
		if task.Status != domain.StatusRunning && task.Status != domain.StatusDispatching {
			now := e.now()
			task.Status = domain.StatusCancelled
			task.FinishedAt = &now
		}
		task.UpdatedAt = e.now()
		return tx.UpdateTask(ctx, task)
	})
}

func (e *Engine) RetryTask(ctx context.Context, taskID int64) error {
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), taskID)
		if err != nil {
			return err
		}
		if task.Status != domain.StatusFailed && task.Status != domain.StatusCancelled {
			return fail(409, "TASK_NOT_RETRYABLE", "只有失败或已取消的任务可以重新排队", nil)
		}
		task.Status = domain.StatusQueued
		task.CancelRequested = false
		task.FinishedAt = nil
		task.LastError = ""
		task.UpdatedAt = e.now()
		return tx.UpdateTask(ctx, task)
	})
}

func (e *Engine) writeUploads(taskID int64, files []Upload) error {
	dir := filepath.Join(e.taskDir(taskID), "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, file := range files {
		name := filepath.Base(strings.TrimSpace(file.Name))
		if name == "." || name == "" || name == string(filepath.Separator) {
			return errString("文件名无效")
		}
		if err := os.WriteFile(filepath.Join(dir, name), file.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) taskDir(id int64) string {
	root := e.WorkRoot
	if root == "" {
		root = "data/work"
	}
	return filepath.Join(root, "tasks", strconv.FormatInt(id, 10))
}

func (e *Engine) queuePosition(ctx context.Context, id int64) int {
	var queued []domain.Task
	_ = e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		queued, err = tx.ListTasks(ctx, domain.TaskFilter{Status: domain.StatusQueued, Limit: 100000})
		return err
	})
	sortTasksFIFO(queued)
	for i, task := range queued {
		if task.ID == id {
			return i + 1
		}
	}
	return 0
}

func sortTasksFIFO(tasks []domain.Task) {
	for i := 1; i < len(tasks); i++ {
		j := i
		for j > 0 {
			a, b := tasks[j-1], tasks[j]
			if a.CreatedAt.Before(b.CreatedAt) || (a.CreatedAt.Equal(b.CreatedAt) && a.ID < b.ID) {
				break
			}
			tasks[j-1], tasks[j] = tasks[j], tasks[j-1]
			j--
		}
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type errString string

func (e errString) Error() string { return string(e) }
