package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type outcome int

const (
	outcomeNone outcome = iota
	outcomeVisual
)

func (uc *TaskUsecase) runExtract(ctx context.Context, shopID, id int) {
	var after func()
	uc.withLock(id, func() {
		ctx, cancel := context.WithTimeout(ctx, uc.rt.CodexTimeout)
		defer cancel()
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil || task.Status != StatusRunning || task.Phase != PhaseExtract {
			return
		}
		result, runErr := uc.runner.Run(ctx, RunRequest{
			Stage:      StageExtract,
			Prompt:     buildModelPrompt(task, StageExtract, ""),
			MasterSKU:  task.MasterSKU,
			WorkDir:    task.ZipLocalPath,
			Attributes: decodeAttrMaps(task.Attributes),
		})
		if runErr != nil {
			if result != nil && result.SessionID != "" {
				uc.rememberSession(ctx, shopID, id, result.SessionID)
			}
			uc.fail(ctx, shopID, id, runErr.Error())
			return
		}
		after = uc.finishRunning(ctx, shopID, id, result, landExtract)
	})
	if after != nil {
		after()
	}
}

func (uc *TaskUsecase) runResume(ctx context.Context, shopID, id int) {
	var after func()
	uc.withLock(id, func() {
		ctx, cancel := context.WithTimeout(ctx, uc.rt.CodexTimeout)
		defer cancel()
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil || task.Status != StatusRunning || task.Phase != PhaseResume {
			return
		}
		result, runErr := uc.runner.Run(ctx, RunRequest{
			Stage:      StageResume,
			SessionID:  task.CodexSessionID,
			Prompt:     buildModelPrompt(task, StageResume, ""),
			MasterSKU:  task.MasterSKU,
			WorkDir:    task.ZipLocalPath,
			Answers:    task.InputValues,
			Attributes: decodeAttrMaps(task.Attributes),
		})
		if runErr != nil {
			if result != nil && result.SessionID != "" {
				uc.rememberSession(ctx, shopID, id, result.SessionID)
			}
			uc.fail(ctx, shopID, id, runErr.Error())
			return
		}
		after = uc.finishRunning(ctx, shopID, id, result, landExtract)
	})
	if after != nil {
		after()
	}
}

func (uc *TaskUsecase) runVisual(ctx context.Context, shopID, id int) {
	uc.withLock(id, func() {
		ctx, cancel := context.WithTimeout(ctx, uc.rt.CodexTimeout)
		defer cancel()
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil || task.Status != StatusRunning || task.Phase != PhaseVisual || !task.TextApproved || task.VisualApproved {
			return
		}
		result, runErr := uc.runner.Run(ctx, RunRequest{
			Stage:     StageVisual,
			SessionID: task.CodexSessionID,
			Prompt:    buildModelPrompt(task, StageVisual, ""),
			MasterSKU: task.MasterSKU,
			WorkDir:   task.ZipLocalPath,
		})
		if runErr != nil {
			if result != nil && result.SessionID != "" {
				uc.rememberSession(ctx, shopID, id, result.SessionID)
			}
			uc.fail(ctx, shopID, id, runErr.Error())
			return
		}
		signal, parseErr := ParseSignal(result.AssistantText())
		if parseErr != nil {
			uc.fail(ctx, shopID, id, parseErr.Error())
			return
		}
		for attempt := 0; attempt < 4; attempt++ {
			task, err = uc.repo.Get(ctx, shopID, id)
			if err != nil || task.Status != StatusRunning || task.Phase != PhaseVisual {
				return
			}
			next := cloneTask(task)
			if result.SessionID != "" {
				next.CodexSessionID = result.SessionID
			}
			if err := applyVisual(next, signal); err != nil {
				uc.fail(ctx, shopID, id, err.Error())
				return
			}
			events := uc.messagesFrom(next, result)
			events = append(events, TaskEvent{Role: RoleSystem, Stage: "视觉方案", Content: "视觉方案已准备。"})
			if next.AutoVisualApprove {
				events = append(events, markVisualPassed(next, true)...)
			} else {
				next.Status = StatusPendingVisual
				next.Phase = PhaseVisualReview
				events = append(events, TaskEvent{Role: RoleSystem, Stage: "审核", Content: "视觉方案已准备，进入方案审核。"})
			}
			saved, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events))
			if isConflict(err) {
				continue
			}
			if err != nil {
				uc.log.Errorf("task %d visual save: %v", id, err)
				return
			}
			if saved.Status == StatusCompleted {
				writeApprovedPlan(saved)
			}
			return
		}
	})
}

func (uc *TaskUsecase) runFollowUp(ctx context.Context, shopID, id int, content string) {
	uc.withLock(id, func() {
		ctx, cancel := context.WithTimeout(ctx, uc.rt.CodexTimeout)
		defer cancel()
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil || task.CodexSessionID == "" {
			return
		}
		result, runErr := uc.runner.Run(ctx, RunRequest{
			Stage:     StageFollowUp,
			SessionID: task.CodexSessionID,
			Prompt:    content,
			MasterSKU: task.MasterSKU,
			WorkDir:   task.ZipLocalPath,
		})
		if runErr != nil {
			uc.appendNote(ctx, shopID, id, "追问失败："+runErr.Error())
			return
		}
		signal, parseErr := ParseSignal(result.AssistantText())
		if parseErr != nil {
			uc.appendNote(ctx, shopID, id, parseErr.Error())
			return
		}
		for attempt := 0; attempt < 4; attempt++ {
			task, err = uc.repo.Get(ctx, shopID, id)
			if err != nil {
				return
			}
			next := cloneTask(task)
			if result.SessionID != "" {
				next.CodexSessionID = result.SessionID
			}
			if signal != nil && signal.Signal == "text_draft" && !next.VisualApproved {
				applyTextDraft(next, signal, result.AssistantText())
			}
			if signal != nil && signal.Signal == "visual_plan" && next.Status != StatusCompleted {
				_ = applyVisual(next, signal)
			}
			events := uc.messagesFrom(next, result)
			if _, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events)); err != nil {
				if isConflict(err) {
					continue
				}
				uc.log.Errorf("task %d follow-up save: %v", id, err)
				return
			}
			return
		}
	})
}

func (uc *TaskUsecase) finishRunning(ctx context.Context, shopID, id int, result *RunResult, land func(*Task, *Signal, string) (outcome, []TaskEvent, error)) func() {
	prose := ""
	if result != nil {
		prose = result.AssistantText()
	}
	signal, err := ParseSignal(prose)
	if err != nil {
		uc.fail(ctx, shopID, id, err.Error())
		return nil
	}
	for attempt := 0; attempt < 4; attempt++ {
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil {
			uc.log.Errorf("task %d: %v", id, err)
			return nil
		}
		if task.Status != StatusRunning {
			return nil
		}
		next := cloneTask(task)
		if result != nil && result.SessionID != "" {
			next.CodexSessionID = result.SessionID
		}
		out, extra, landErr := land(next, signal, prose)
		if landErr != nil {
			uc.fail(ctx, shopID, id, landErr.Error())
			return nil
		}
		events := append(uc.messagesFrom(next, result), extra...)
		if _, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, events)); err != nil {
			if isConflict(err) {
				continue
			}
			uc.log.Errorf("task %d save: %v", id, err)
			return nil
		}
		if out == outcomeVisual {
			return func() {
				uc.schedule(func() { uc.runVisual(context.Background(), shopID, id) })
			}
		}
		return nil
	}
	uc.fail(ctx, shopID, id, "任务版本连续冲突，已停止这次运行。")
	return nil
}

func landExtract(task *Task, signal *Signal, prose string) (outcome, []TaskEvent, error) {
	if signal == nil {
		return outcomeNone, nil, fmt.Errorf("Codex 输出缺少 ozon-signal")
	}
	switch signal.Signal {
	case "need_input":
		if err := applyNeed(task, signal); err != nil {
			return outcomeNone, nil, err
		}
		return outcomeNone, []TaskEvent{{Role: RoleSystem, Stage: "补充", Content: "需要补充数据后才能继续。"}}, nil
	case "text_draft":
		return landText(task, signal, prose)
	default:
		return outcomeNone, nil, fmt.Errorf("当前阶段收到了无法处理的信号 %s", signal.Signal)
	}
}

func landText(task *Task, signal *Signal, prose string) (outcome, []TaskEvent, error) {
	applyTextDraft(task, signal, prose)
	events := []TaskEvent{{Role: RoleSystem, Stage: "文案", Content: "文案已准备。"}}
	if task.AutoTextApprove {
		events = append(events, markTextPassed(task, true)...)
		return outcomeVisual, events, nil
	}
	task.Status = StatusPendingText
	task.Phase = PhaseTextReview
	events = append(events, TaskEvent{Role: RoleSystem, Stage: "审核", Content: "文案已准备，进入文案审核。"})
	return outcomeNone, events, nil
}

func applyNeed(task *Task, signal *Signal) error {
	fields := make([]MissingField, 0, len(signal.MissingFields))
	for _, field := range signal.MissingFields {
		field.Key = strings.TrimSpace(field.Key)
		field.Label = strings.TrimSpace(field.Label)
		if field.Key == "" {
			continue
		}
		if field.Label == "" {
			field.Label = field.Key
		}
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		return fmt.Errorf("Codex 要求补充数据，但没有给出字段清单")
	}
	task.MissingFields = fields
	task.Status = StatusNeedInput
	task.Phase = PhaseAwaitInput
	return nil
}

func applyTextDraft(task *Task, signal *Signal, prose string) {
	skus := MergeSubSKUs(task.MasterSKU, signal.SubSKUs, prose)
	draft := TextDraft{
		TitleRU:       signal.TitleRU,
		SellingPoints: signal.SellingPoints,
		Attributes:    signal.Attributes,
		SubSKUs:       skus,
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		raw = []byte(`{}`)
	}
	task.TextDraft = raw
	task.SubSKUs = skus
	task.MissingFields = nil
}

func applyVisual(task *Task, signal *Signal) error {
	if signal == nil || signal.Signal != "visual_plan" || len(signal.Items) == 0 {
		return fmt.Errorf("Codex 输出缺少 visual_plan")
	}
	raw, err := json.Marshal(VisualPlan{Items: signal.Items})
	if err != nil {
		return err
	}
	task.VisualPlan = raw
	return nil
}

func (uc *TaskUsecase) fail(ctx context.Context, shopID, id int, message string) {
	message = truncateRunes(strings.TrimSpace(Redact(message)), 1800)
	if message == "" {
		message = "任务失败"
	}
	for attempt := 0; attempt < 4; attempt++ {
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil || task.Status == StatusFailed || task.Status == StatusCompleted {
			return
		}
		next := cloneTask(task)
		next.Status = StatusFailed
		next.Phase = PhaseFailed
		next.ErrorMessage = message
		if _, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, []TaskEvent{{
			Role: RoleSystem, Stage: "错误", Content: message,
		}})); err != nil {
			if isConflict(err) {
				continue
			}
			return
		}
		return
	}
}

func (uc *TaskUsecase) rememberSession(ctx context.Context, shopID, id int, sessionID string) {
	if sessionID == "" {
		return
	}
	task, err := uc.repo.Get(ctx, shopID, id)
	if err != nil || task.CodexSessionID != "" {
		return
	}
	next := cloneTask(task)
	next.CodexSessionID = sessionID
	_, _ = uc.repo.Save(ctx, task.Revision, next, nil)
}

func (uc *TaskUsecase) appendNote(ctx context.Context, shopID, id int, message string) {
	message = truncateRunes(Redact(message), 1800)
	for attempt := 0; attempt < 4; attempt++ {
		task, err := uc.repo.Get(ctx, shopID, id)
		if err != nil {
			return
		}
		next := cloneTask(task)
		if _, err := uc.repo.Save(ctx, task.Revision, next, stamp(next.CodexSessionID, []TaskEvent{{
			Role: RoleSystem, Stage: "错误", Content: message,
		}})); err == nil || !isConflict(err) {
			return
		}
	}
}

func (uc *TaskUsecase) messagesFrom(task *Task, result *RunResult) []TaskEvent {
	if result == nil {
		return nil
	}
	events := make([]TaskEvent, 0, len(result.Messages))
	for _, msg := range result.Messages {
		content, ref := uc.spill(task, Redact(msg.Content))
		events = append(events, TaskEvent{
			SessionID:   task.CodexSessionID,
			Role:        msg.Role,
			Stage:       msg.Stage,
			Content:     content,
			ToolCallID:  msg.ToolCallID,
			ResourceRef: ref,
		})
	}
	return events
}

func (uc *TaskUsecase) spill(task *Task, content string) (string, string) {
	if len(content) <= 32768 || task.ZipLocalPath == "" {
		return content, ""
	}
	name := filepath.ToSlash(filepath.Join("events", fmt.Sprintf("%d.txt", time.Now().UnixNano())))
	full := filepath.Join(task.ZipLocalPath, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return truncateRunes(content, 4000), ""
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return truncateRunes(content, 4000), ""
	}
	preview := truncateRunes(content, 4000) + "\n…（完整内容见资源 " + name + "）"
	return preview, name
}

func buildModelPrompt(task *Task, stage, follow string) string {
	switch stage {
	case StageExtract:
		return task.RenderedPrompt + "\n\n" + Protocol + "\n\n现在解析素材目录。若缺少必填属性，只输出 need_input。否则输出 text_draft，子 SKU 形如 " + task.MasterSKU + "-1。"
	case StageResume:
		raw, _ := json.MarshalIndent(task.InputValues, "", "  ")
		return "用户已补充这些字段，请继续同一会话并输出 text_draft：\n" + string(raw) + "\n\n" + Protocol
	case StageVisual:
		return "文案审核已通过。请继续同一会话，为每个子 SKU 输出 visual_plan。\n\n" + Protocol
	default:
		return follow
	}
}

func decodeAttrMaps(raw json.RawMessage) []map[string]any {
	if len(bytesTrim(raw)) == 0 {
		return nil
	}
	var attrs []map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil
	}
	return attrs
}

func bytesTrim(raw json.RawMessage) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func writeApprovedPlan(task *Task) {
	if task == nil || task.ZipLocalPath == "" {
		return
	}
	payload := map[string]any{
		"master_sku":  task.MasterSKU,
		"sub_skus":    task.SubSKUs,
		"text_draft":  json.RawMessage(emptyRaw(task.TextDraft, `{}`)),
		"visual_plan": json.RawMessage(emptyRaw(task.VisualPlan, `{}`)),
		"note":        "刊登记录由模块 4 生成。这里只保留已审核的成品快照。",
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(task.ZipLocalPath, "approved_plan.json"), raw, 0o644)
}

func emptyRaw(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}
