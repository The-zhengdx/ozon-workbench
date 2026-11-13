package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/store"
)

type configFailRunner struct {
	eng *Engine
	id  *int64
}

func (r configFailRunner) Exec(ctx context.Context, _ codexcli.ExecRequest) (codexcli.ExecResult, error) {
	if r.eng != nil && r.id != nil && *r.id != 0 {
		_ = r.eng.DB.InTx(ctx, func(tx store.Tx) error {
			task, err := tx.GetTask(ctx, *r.id)
			if err != nil {
				return err
			}
			task.CancelRequested = true
			return tx.UpdateTask(ctx, task)
		})
	}
	return codexcli.ExecResult{
		Args:    []string{"node", `C:\npm\node_modules\@openai\codex\bin\codex.js`, "exec", "--json", "-"},
		Started: false,
	}, fmt.Errorf("Codex 参数无效：第 1 个参数含有换行或空字符，不能交给 cmd.exe")
}

func TestConfigFailureStaysFailedWhenCancelRaces(t *testing.T) {
	fix := newEngine(t)
	eng := fix.eng
	var id int64
	eng.Codex = configFailRunner{eng: eng, id: &id}
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 2, 9, "类目提示词")
	task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 2, TypeID: 9, BaseSKU: "HAT-1"})
	if err != nil {
		t.Fatal(err)
	}
	id = task.ID
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := eng.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("status = %s err=%s", got.Status, got.LastError)
	}
	if !strings.Contains(got.LastError, "Codex 参数无效") || !strings.Contains(got.LastError, "exit=0") || !strings.Contains(got.LastError, "argv:") {
		t.Fatalf("last error = %s", got.LastError)
	}
	msgs, err := eng.Messages(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Kind == domain.KindSystemNote && strings.Contains(msg.Content, "codex.js") && msg.DeliveryStatus == domain.DeliveryNotApplicable {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing system event: %+v", msgs)
	}
	if err := eng.CancelTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	again, err := eng.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != domain.StatusFailed || !strings.Contains(again.LastError, "Codex 参数无效") {
		t.Fatalf("cancel rewrote failure: %s %s", again.Status, again.LastError)
	}
}
