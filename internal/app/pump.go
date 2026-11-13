package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/media"
	"ozon-workbench/internal/store"
)

func (e *Engine) Recover(ctx context.Context) error {
	var tasks []domain.Task
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		tasks, err = tx.ListTasks(ctx, domain.TaskFilter{Limit: 100000})
		return err
	})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		switch task.Status {
		case domain.StatusDispatching, domain.StatusRunning, domain.StatusUploadingMedia, domain.StatusFinalizing:
			if err := e.recoverOne(ctx, task.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) recoverOne(ctx context.Context, id int64) error {
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), id)
		if err != nil {
			return err
		}
		turns, err := tx.ListTurns(ctx, id)
		if err != nil {
			return err
		}
		active, ok, err := tx.ActiveTurn(ctx, id)
		if err != nil {
			return err
		}
		now := e.now()
		if task.Status == domain.StatusDispatching && len(turns) == 0 {
			task.Status = domain.StatusQueued
			task.AdmittedAt = nil
			task.UpdatedAt = now
			return tx.UpdateTask(ctx, task)
		}
		if ok {
			active.RunStatus = domain.TurnUncertain
			active.ErrorMessage = "进程中断，轮次结果不确定"
			active.FinishedAt = &now
			if err := tx.UpdateTurn(ctx, active); err != nil {
				return err
			}
			task.LastError = active.ErrorMessage
			task.UpdatedAt = now
			return tx.UpdateTask(ctx, task)
		}
		if task.Status == domain.StatusFinalizing {
			return e.finishIfDelivered(ctx, tx, task)
		}
		return nil
	})
}

func (e *Engine) finishIfDelivered(ctx context.Context, tx store.Tx, task domain.Task) error {
	if len(task.LastReplyJSON) == 0 {
		return nil
	}
	reply, err := codexcli.ParseReply(task.LastReplyJSON)
	if err != nil || reply.Status != "completed" {
		return nil
	}
	rows, err := tx.ListDeliveries(ctx, task.ID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, row := range rows {
		have[row.SKURef] = true
	}
	for _, product := range reply.Data.Products {
		if !have[product.SKURef] {
			return nil
		}
	}
	now := e.now()
	task.Status = domain.StatusCompleted
	task.Stage = "delivery"
	task.FinishedAt = &now
	task.UpdatedAt = now
	return tx.UpdateTask(ctx, task)
}

// Pump 领取队列并推进能安全推进的任务。不确定的轮次不会自动再开一轮。
func (e *Engine) Pump(ctx context.Context) error {
	if err := e.Recover(ctx); err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		moved, err := e.step(ctx)
		if err != nil {
			return err
		}
		if !moved {
			return nil
		}
	}
	return nil
}

func (e *Engine) step(ctx context.Context) (bool, error) {
	var claimed []domain.Task
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		claimed, err = tx.ClaimQueued(ctx, e.now())
		return err
	})
	if err != nil {
		return false, err
	}
	moved := len(claimed) > 0
	var tasks []domain.Task
	if err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		tasks, err = tx.ListTasks(ctx, domain.TaskFilter{Limit: 100000})
		return err
	}); err != nil {
		return false, err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	for _, task := range tasks {
		if task.CancelRequested && task.Status != domain.StatusCancelled && task.Status != domain.StatusRunning {
			if err := e.markCancelled(ctx, task.ID); err != nil {
				return false, err
			}
			moved = true
			continue
		}
		switch task.Status {
		case domain.StatusUploadingMedia:
			ok, err := e.uploadMedia(ctx, task.ID)
			if err != nil {
				return false, err
			}
			moved = moved || ok
		case domain.StatusFinalizing:
			ok, err := e.finalize(ctx, task.ID)
			if err != nil {
				return false, err
			}
			moved = moved || ok
		case domain.StatusDispatching, domain.StatusRunning, domain.StatusWaitingInput, domain.StatusWaitingReview:
			ok, err := e.maybeTurn(ctx, task.ID)
			if err != nil {
				return false, err
			}
			moved = moved || ok
		}
	}
	return moved, nil
}

func (e *Engine) maybeTurn(ctx context.Context, id int64) (bool, error) {
	var run bool
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(ctx, id)
		if err != nil {
			return err
		}
		if uncertain(task, tx, ctx) {
			return nil
		}
		_, active, err := tx.ActiveTurn(ctx, id)
		if err != nil || active {
			return err
		}
		pending, err := tx.PendingMessages(ctx, id)
		if err != nil {
			return err
		}
		switch task.Status {
		case domain.StatusDispatching, domain.StatusRunning:
			run = true
		case domain.StatusWaitingInput, domain.StatusWaitingReview:
			run = len(pending) > 0
		}
		return nil
	})
	if err != nil || !run {
		return false, err
	}
	if err := e.runTurn(ctx, id); err != nil {
		return true, err
	}
	return true, nil
}

func uncertain(task domain.Task, tx store.Tx, ctx context.Context) bool {
	turns, err := tx.ListTurns(ctx, task.ID)
	if err != nil || len(turns) == 0 {
		return false
	}
	last := turns[len(turns)-1]
	return last.RunStatus == domain.TurnUncertain
}

func (e *Engine) runTurn(ctx context.Context, id int64) error {
	turnCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	if e.cancels == nil {
		e.cancels = map[int64]context.CancelFunc{}
	}
	e.cancels[id] = cancel
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		delete(e.cancels, id)
		e.mu.Unlock()
	}()

	var task domain.Task
	var turn domain.Turn
	var prompt string
	var included []int64
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		task, err = tx.GetTask(store.ForUpdate(ctx), id)
		if err != nil {
			return err
		}
		if task.CancelRequested {
			return e.cancelLocked(ctx, tx, task)
		}
		_, active, err := tx.ActiveTurn(ctx, id)
		if err != nil || active || uncertain(task, tx, ctx) {
			return err
		}
		turns, err := tx.ListTurns(ctx, id)
		if err != nil {
			return err
		}
		pending, err := tx.PendingMessages(ctx, id)
		if err != nil {
			return err
		}
		maxRev := task.AppliedRequirementRevision
		for _, msg := range pending {
			if msg.RequirementRevision != nil && *msg.RequirementRevision > maxRev {
				maxRev = *msg.RequirementRevision
			}
		}
		now := e.now()
		turn, err = tx.CreateTurn(ctx, domain.Turn{
			TaskID: id, Seq: len(turns) + 1, RunStatus: domain.TurnRunning,
			InputMaxRequirementRevision: maxRev, StartedAt: now,
		})
		if err != nil {
			return err
		}
		for _, msg := range pending {
			msg.DeliveryStatus = domain.DeliverySent
			msg.TurnID = &turn.ID
			if err := tx.UpdateMessage(ctx, msg); err != nil {
				return err
			}
			included = append(included, msg.ID)
		}
		task.Status = domain.StatusRunning
		task.UpdatedAt = now
		prompt = e.prompt(task, pending)
		return tx.UpdateTask(ctx, task)
	})
	if err != nil || prompt == "" {
		return err
	}
	schema, err := codexcli.SchemaPath(filepath.Join(e.taskDir(id), "schema"))
	if err != nil {
		return err
	}
	out := filepath.Join(e.taskDir(id), fmt.Sprintf("turn-%03d.json", turn.Seq))
	binary := "codex"
	_ = e.DB.InTx(ctx, func(tx store.Tx) error {
		binary = configString(ctx, tx, "codex_binary", "codex")
		return nil
	})
	result, runErr := e.Codex.Exec(turnCtx, codexcli.ExecRequest{
		Binary: binary, WorkDir: e.taskDir(id), SessionID: task.CodexSessionID,
		Prompt: prompt, SchemaPath: schema, OutputPath: out,
	})
	return e.finishTurn(ctx, id, turn.ID, included, out, result, runErr)
}

func (e *Engine) finishTurn(ctx context.Context, taskID, turnID int64, included []int64, out string, result codexcli.ExecResult, runErr error) error {
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), taskID)
		if err != nil {
			return err
		}
		turn, err := tx.ListTurns(ctx, taskID)
		if err != nil {
			return err
		}
		var current domain.Turn
		for _, item := range turn {
			if item.ID == turnID {
				current = item
			}
		}
		now := e.now()
		current.FinishedAt = &now
		current.OutputFile = out
		if result.SessionID != "" {
			task.CodexSessionID = result.SessionID
		}
		if task.CancelRequested {
			current.RunStatus = domain.TurnFailed
			current.ErrorMessage = "已取消"
			_ = tx.UpdateTurn(ctx, current)
			return e.cancelLocked(ctx, tx, task)
		}
		reply, reason := parseTurn(result, runErr)
		if reason != "" {
			current.RunStatus = domain.TurnFailed
			current.ExitCode = &result.ExitCode
			current.ErrorMessage = reason
			if err := tx.UpdateTurn(ctx, current); err != nil {
				return err
			}
			if err := e.markIncluded(ctx, tx, taskID, included, domain.DeliveryUncertain, now); err != nil {
				return err
			}
			failed, err := e.failureCount(ctx, tx, taskID)
			if err != nil {
				return err
			}
			limit := configInt(ctx, tx, "max_infra_retries", 0)
			task.LastError = reason
			task.UpdatedAt = now
			if failed > limit {
				task.Status = domain.StatusFailed
				task.FinishedAt = &now
			} else {
				task.Status = domain.StatusRunning
			}
			return tx.UpdateTask(ctx, task)
		}
		raw, _ := json.Marshal(reply)
		current.RunStatus = domain.TurnSucceeded
		current.ReplyJSON = raw
		current.ExitCode = &result.ExitCode
		if err := tx.UpdateTurn(ctx, current); err != nil {
			return err
		}
		if err := e.markIncluded(ctx, tx, taskID, included, domain.DeliveryAccepted, now); err != nil {
			return err
		}
		if reply.Stage != "" {
			task.Stage = reply.Stage
		}
		task.LastReplyJSON = raw
		task.AppliedRequirementRevision = current.InputMaxRequirementRevision
		task.LastError = ""
		task.UpdatedAt = now
		pending, err := tx.PendingMessages(ctx, taskID)
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			task.Status = domain.StatusRunning
			return tx.UpdateTask(ctx, task)
		}
		return e.applyReply(ctx, tx, task, reply, now)
	})
}

func parseTurn(result codexcli.ExecResult, runErr error) (codexcli.Reply, string) {
	if runErr != nil {
		return codexcli.Reply{}, runErr.Error()
	}
	if len(bytesTrim(result.Output)) == 0 {
		return codexcli.Reply{}, "Codex 没有写出业务 JSON"
	}
	if err := codexcli.ValidateSchema(codexcli.SchemaJSON, result.Output); err != nil {
		return codexcli.Reply{}, err.Error()
	}
	reply, err := codexcli.ParseReply(result.Output)
	if err != nil {
		return codexcli.Reply{}, err.Error()
	}
	if err := codexcli.BusinessError(reply); err != nil {
		return codexcli.Reply{}, err.Error()
	}
	return reply, ""
}

func (e *Engine) applyReply(ctx context.Context, tx store.Tx, task domain.Task, reply codexcli.Reply, now time.Time) error {
	switch reply.Status {
	case "needs_input":
		task.Status = domain.StatusWaitingInput
	case "pending_review":
		kind := ""
		if reply.Review != nil {
			kind = reply.Review.Kind
		}
		if autoKind(task, kind) {
			task = approveKind(task, kind, true)
			rev := task.RequirementRevision
			_, err := tx.CreateMessage(ctx, domain.Message{
				TaskID: task.ID, Role: domain.RoleSystem, Kind: domain.KindReviewDecision,
				Content: "自动通过 " + kind, RequirementRevision: &rev,
				DeliveryStatus: domain.DeliveryPending, CreatedAt: now,
			})
			if err != nil {
				return err
			}
			task.Status = domain.StatusRunning
		} else {
			task.Status = domain.StatusWaitingReview
		}
	case "media_ready":
		if !task.VisualResultApproved && !task.VisualResultAutoApprove {
			task.Status = domain.StatusWaitingReview
		} else if err := e.mediaFilesExist(task, reply); err != nil {
			task.Status = domain.StatusFailed
			task.LastError = err.Error()
			task.FinishedAt = &now
		} else {
			task.Status = domain.StatusUploadingMedia
		}
	case "completed":
		if !approvalsOK(task) {
			task.Status = domain.StatusWaitingReview
			task.LastError = "交付前还缺审核通过记录"
		} else {
			task.Status = domain.StatusFinalizing
		}
	case "failed":
		task.Status = domain.StatusFailed
		task.FinishedAt = &now
		if reply.Error != nil {
			task.LastError = reply.Error.Code + ": " + reply.Error.Message
		}
	default:
		return e.failOrRetry(ctx, tx, task, "未知业务状态", now)
	}
	task.UpdatedAt = now
	_, err := tx.CreateMessage(ctx, domain.Message{
		TaskID: task.ID, Role: domain.RoleAssistant, Kind: domain.KindCodexReply,
		Content: reply.Message, Payload: task.LastReplyJSON, DeliveryStatus: domain.DeliveryNotApplicable, CreatedAt: now,
	})
	if err != nil {
		return err
	}
	return tx.UpdateTask(ctx, task)
}

func (e *Engine) failOrRetry(ctx context.Context, tx store.Tx, task domain.Task, reason string, now time.Time) error {
	task.LastError = reason
	failed, err := e.failureCount(ctx, tx, task.ID)
	if err != nil {
		return err
	}
	if failed > configInt(ctx, tx, "max_infra_retries", 0) {
		task.Status = domain.StatusFailed
		task.FinishedAt = &now
	} else {
		task.Status = domain.StatusRunning
	}
	task.UpdatedAt = now
	return tx.UpdateTask(ctx, task)
}

func (e *Engine) mediaFilesExist(task domain.Task, reply codexcli.Reply) error {
	for _, file := range reply.Data.Media {
		if _, err := media.Resolve(e.taskDir(task.ID), file.FilePath); err != nil {
			return fmt.Errorf("素材 %s 不可用：%s", file.AssetID, err.Error())
		}
	}
	return nil
}

func (e *Engine) failureCount(ctx context.Context, tx store.Tx, taskID int64) (int, error) {
	turns, err := tx.ListTurns(ctx, taskID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, turn := range turns {
		if turn.RunStatus == domain.TurnFailed {
			n++
		}
	}
	return n, nil
}

func (e *Engine) markIncluded(ctx context.Context, tx store.Tx, taskID int64, ids []int64, status string, now time.Time) error {
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	msgs, err := tx.ListMessages(ctx, taskID)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		if !want[msg.ID] {
			continue
		}
		msg.DeliveryStatus = status
		msg.ProcessedAt = &now
		if err := tx.UpdateMessage(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func autoKind(task domain.Task, kind string) bool {
	switch kind {
	case "copy":
		return task.CopyAutoApprove
	case "visual_plan":
		return task.VisualPlanAutoApprove
	case "visual_result", "final":
		return task.VisualResultAutoApprove
	default:
		return false
	}
}

func approveKind(task domain.Task, kind string, ok bool) domain.Task {
	switch kind {
	case "copy":
		task.CopyApproved = ok
	case "visual_plan":
		task.VisualPlanApproved = ok
	case "visual_result", "final":
		task.VisualResultApproved = ok
		if kind == "final" {
			task.CopyApproved = ok
			task.VisualPlanApproved = ok
		}
	}
	return task
}

func approvalsOK(task domain.Task) bool {
	return (task.CopyApproved || task.CopyAutoApprove) &&
		(task.VisualPlanApproved || task.VisualPlanAutoApprove) &&
		(task.VisualResultApproved || task.VisualResultAutoApprove)
}

func (e *Engine) cancelLocked(ctx context.Context, tx store.Tx, task domain.Task) error {
	now := e.now()
	task.Status = domain.StatusCancelled
	task.CancelRequested = true
	task.FinishedAt = &now
	task.UpdatedAt = now
	return tx.UpdateTask(ctx, task)
}

func (e *Engine) markCancelled(ctx context.Context, id int64) error {
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(ctx, id)
		if err != nil {
			return err
		}
		return e.cancelLocked(ctx, tx, task)
	})
}

func (e *Engine) prompt(task domain.Task, pending []domain.Message) string {
	var b strings.Builder
	b.WriteString("你是 Ozon 商品加工助手。只把最终业务结果写入 --output-schema 指定的文件。不要用自然语言代替 JSON。不要猜测采购成本或国内运费，缺失就返回 needs_input。不要计算最终售价。不要改 description_category_id 或 type_id。不要编造字典 ID 或 OSS URL。\n")
	b.WriteString("查询类目属性时使用 curl.exe -sS，并带上下面的只读令牌。示例：\n")
	b.WriteString(fmt.Sprintf("curl.exe -sS -H \"Authorization: Bearer %s\" \"%s/api/categories/%d/types/%d/attributes?language=RU\"\n",
		task.CategoryToken, e.apiBase(), task.DescriptionCategoryID, task.TypeID))
	b.WriteString("\n类目提示词：\n")
	b.WriteString(task.PromptSnapshot)
	b.WriteString("\n\n任务上下文：\n")
	fmt.Fprintf(&b, "task_id=%d\nshop_id=%d\ndescription_category_id=%d\ntype_id=%d\nbase_sku=%s\norigin_link=%s\n",
		task.ID, task.ShopID, task.DescriptionCategoryID, task.TypeID, task.BaseSKU, task.OriginLink)
	entries, _ := os.ReadDir(filepath.Join(e.taskDir(task.ID), "inbox"))
	for _, entry := range entries {
		fmt.Fprintf(&b, "素材文件 inbox/%s\n", entry.Name())
	}
	if len(pending) > 0 {
		b.WriteString("\n本轮必须先处理以下排队消息，按顺序：\n")
		for _, msg := range pending {
			fmt.Fprintf(&b, "[%s/%s] %s\n", msg.Role, msg.Kind, msg.Content)
			if len(msg.Payload) > 0 {
				b.Write(msg.Payload)
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
