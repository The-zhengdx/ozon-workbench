package biz

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

type inlineSched struct{}

func (inlineSched) Go(fn func()) { fn() }

type memRepo struct {
	mu     sync.Mutex
	nextID int
	tasks  map[int]*Task
	events map[int][]TaskEvent
	nextEv int
}

func newMemRepo() *memRepo {
	return &memRepo{nextID: 1, tasks: map[int]*Task{}, events: map[int][]TaskEvent{}, nextEv: 1}
}

func (m *memRepo) Create(_ context.Context, task *Task, events []TaskEvent) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := cloneTask(task)
	stored.ID = m.nextID
	m.nextID++
	if stored.Revision == 0 {
		stored.Revision = 1
	}
	now := time.Now().UTC()
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = now
	}
	stored.UpdatedAt = now
	m.tasks[stored.ID] = stored
	m.appendLocked(stored, events)
	return cloneTask(stored), nil
}

func (m *memRepo) Get(_ context.Context, shopID, id int) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	if task == nil || task.ShopID != shopID {
		return nil, notFound("任务不存在。")
	}
	return cloneTask(task), nil
}

func (m *memRepo) GetByID(_ context.Context, id int) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	if task == nil {
		return nil, notFound("任务不存在。")
	}
	return cloneTask(task), nil
}

func (m *memRepo) List(_ context.Context, shopID int, status string, page, pageSize int) ([]*Task, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []*Task
	for id := m.nextID - 1; id >= 1; id-- {
		task := m.tasks[id]
		if task == nil || task.ShopID != shopID {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		rows = append(rows, cloneTask(task))
	}
	total := len(rows)
	start := (page - 1) * pageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end], total, nil
}

func (m *memRepo) ListRecoverable(context.Context) ([]*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Task
	for _, task := range m.tasks {
		if task.Status == StatusRunning ||
			(task.Status == StatusPendingText && task.AutoTextApprove && !task.TextApproved) ||
			(task.Status == StatusPendingVisual && task.AutoVisualApprove && !task.VisualApproved) {
			out = append(out, cloneTask(task))
		}
	}
	return out, nil
}

func (m *memRepo) Save(_ context.Context, expected int64, task *Task, events []TaskEvent) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.tasks[task.ID]
	if current == nil || current.ShopID != task.ShopID {
		return nil, notFound("任务不存在。")
	}
	if current.Revision != expected {
		return nil, revisionConflict()
	}
	stored := cloneTask(task)
	stored.Revision = expected + 1
	stored.UpdatedAt = time.Now().UTC()
	stored.CreatedAt = current.CreatedAt
	m.tasks[stored.ID] = stored
	m.appendLocked(stored, events)
	return cloneTask(stored), nil
}

func (m *memRepo) ListEvents(_ context.Context, taskID int, query EventQuery) ([]TaskEvent, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var filtered []TaskEvent
	for _, event := range m.events[taskID] {
		if query.AfterSeq > 0 && event.Seq <= query.AfterSeq {
			continue
		}
		if query.BeforeSeq > 0 && event.Seq >= query.BeforeSeq {
			continue
		}
		filtered = append(filtered, event)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	hasMore := false
	if query.BeforeSeq > 0 && query.AfterSeq == 0 {
		if len(filtered) > limit {
			hasMore = true
			filtered = filtered[len(filtered)-limit:]
		}
	} else if len(filtered) > limit {
		hasMore = true
		filtered = filtered[:limit]
	}
	out := make([]TaskEvent, len(filtered))
	copy(out, filtered)
	return out, hasMore, nil
}

func (m *memRepo) appendLocked(task *Task, events []TaskEvent) {
	seq := 0
	if existing := m.events[task.ID]; len(existing) > 0 {
		seq = existing[len(existing)-1].Seq
	}
	for _, event := range events {
		seq++
		event.ID = m.nextEv
		m.nextEv++
		event.TaskID = task.ID
		event.ShopID = task.ShopID
		event.Seq = seq
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		m.events[task.ID] = append(m.events[task.ID], event)
	}
}

type fakeShops struct{ shop *Shop }

func (f fakeShops) List(context.Context) ([]*Shop, error) { return []*Shop{f.shop}, nil }
func (f fakeShops) Get(_ context.Context, id int) (*Shop, error) {
	if f.shop == nil || f.shop.ID != id {
		return nil, notFound("店铺不存在。")
	}
	return f.shop, nil
}
func (f fakeShops) GetByClientID(context.Context, string) (*Shop, error) { return nil, nil }
func (f fakeShops) Create(context.Context, *Shop) (*Shop, error)         { return f.shop, nil }
func (f fakeShops) Update(context.Context, *Shop) (*Shop, error)         { return f.shop, nil }
func (f fakeShops) SetDefault(context.Context, int) error                { return nil }

type fakeCatalog struct {
	node   FlatNode
	prompt *Prompt
	attrs  *AttrSnapshot
}

func (f fakeCatalog) ListNodes(context.Context, string) ([]FlatNode, error) {
	return []FlatNode{f.node}, nil
}
func (f fakeCatalog) GetPrompt(context.Context, int64, int64) (*Prompt, error) { return f.prompt, nil }
func (f fakeCatalog) LatestAttributes(context.Context, int64, int64, string) (*AttrSnapshot, error) {
	return f.attrs, nil
}

func testUC(t *testing.T, runner CodexRunner) (*TaskUsecase, *memRepo, string) {
	t.Helper()
	root := t.TempDir()
	repo := newMemRepo()
	uc, err := NewTaskUsecase(repo, fakeShops{shop: &Shop{ID: 7, Name: "汽配店"}}, fakeCatalog{
		node: FlatNode{
			LocalID: 11, DescriptionCategoryID: 17028922, TypeID: 97062518,
			Name: "车载手机支架", IsLeaf: true, Path: []string{"汽车用品", "车载配件", "车载手机支架"},
		},
		prompt: &Prompt{CodexPrompt: "按 {{master_sku}} 生成子 SKU，并写俄文标题。"},
		attrs: &AttrSnapshot{Attributes: []map[string]any{
			{"id": 85, "name": "品牌", "is_required": true},
		}},
	}, runner, inlineSched{}, TaskRuntime{MediaRoot: root, MaxZipBytes: 1 << 20, MaxUncompressed: 1 << 20, MaxFiles: 20, CodexTimeout: time.Minute}, log.NewStdLogger(os.Stdout))
	if err != nil {
		t.Fatal(err)
	}
	return uc, repo, root
}

func writeSampleZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTaskFlowNeedInputReviewAndFollowUp(t *testing.T) {
	uc, repo, root := testUC(t, NewMockRunner())
	ctx := context.Background()
	zipPath := filepath.Join(root, "incoming", "1688.zip")
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSampleZip(t, zipPath, map[string]string{"详情.html": "<html>支架</html>", "图片/主图.jpg": "img"})
	created, err := uc.Create(ctx, CreateInput{
		ShopID: 7, MasterSKU: "30", SupplierURL: "https://detail.1688.com/offer/1.html",
		DescriptionCategoryID: 17028922, TypeID: 97062518, ZipPath: zipPath, ZipFilename: "1688.zip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatal("uploaded zip should be deleted after extraction")
	}
	if created.Status != StatusNeedInput {
		t.Fatalf("status = %s", created.Status)
	}
	if created.ZipLocalPath == "" {
		t.Fatal("zip_local_path was not stored")
	}
	if _, err := os.Stat(filepath.Join(created.ZipLocalPath, "图片", "主图.jpg")); err != nil {
		t.Fatal(err)
	}
	if len(created.MissingFields) != 1 || created.MissingFields[0].Label != "品牌" {
		t.Fatalf("missing = %#v", created.MissingFields)
	}
	if !strings.Contains(created.RenderedPrompt, "按 30 生成子 SKU") || strings.Contains(created.RenderedPrompt, "{{master_sku}}") {
		t.Fatalf("rendered prompt = %s", created.RenderedPrompt)
	}
	events, err := uc.Events(ctx, 7, created.ID, EventQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	joined := joinEventText(events.Events)
	if !strings.Contains(joined, "主 SKU：30") || !strings.Contains(joined, "read_material_manifest") || !strings.Contains(joined, "按 30 生成子 SKU") {
		t.Fatalf("timeline missing the opening turn: %s", joined)
	}
	session := created.CodexSessionID
	if session == "" {
		t.Fatal("session id was not persisted")
	}

	pending, err := uc.SubmitInput(ctx, 7, created.ID, created.Revision, map[string]string{"attr_85": "OZ"})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != StatusPendingText {
		t.Fatalf("after input status = %s, error = %s", pending.Status, pending.ErrorMessage)
	}
	if len(pending.SubSKUs) != 2 || pending.SubSKUs[0].OfferID != "30-1" || pending.SubSKUs[1].OfferID != "30-2" {
		t.Fatalf("sub skus = %#v", pending.SubSKUs)
	}

	visual, err := uc.Approve(ctx, 7, pending.ID, "text", pending.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if visual.Status != StatusPendingVisual || visual.TextApprovedAuto {
		t.Fatalf("visual gate = %+v", visual)
	}
	done, err := uc.Approve(ctx, 7, visual.ID, "visual", visual.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusCompleted || done.CodexSessionID != session {
		t.Fatalf("completed = %+v session %s", done.Status, done.CodexSessionID)
	}
	if _, err := os.Stat(filepath.Join(done.ZipLocalPath, "approved_plan.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Approve(ctx, 7, done.ID, "visual", done.Revision); err == nil {
		t.Fatal("approving a finished task should fail")
	}

	followed, err := uc.FollowUp(ctx, 7, done.ID, done.Revision, "请列出当前货号")
	if err != nil {
		t.Fatal(err)
	}
	if followed.Status != StatusCompleted || followed.CodexSessionID != session {
		t.Fatalf("follow-up changed the task: %+v", followed)
	}
	page, err := uc.Events(ctx, 7, done.ID, EventQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	text := joinEventText(page.Events)
	if !strings.Contains(text, "请列出当前货号") || !strings.Contains(text, "当前主 SKU 为 30") {
		t.Fatalf("follow-up missing from timeline: %s", text)
	}
	if page.Events[0].Seq != 1 {
		t.Fatalf("timeline did not start at the first event: %#v", page.Events[0])
	}

	otherZip := filepath.Join(root, "incoming", "other.zip")
	writeSampleZip(t, otherZip, map[string]string{"a.txt": "a"})
	other, err := uc.Create(ctx, CreateInput{
		ShopID: 7, MasterSKU: "41", DescriptionCategoryID: 17028922, TypeID: 97062518,
		ZipPath: otherZip, ZipFilename: "other.zip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if other.CodexSessionID == session {
		t.Fatal("tasks shared a codex session")
	}
	otherEvents, err := uc.Events(ctx, 7, other.ID, EventQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joinEventText(otherEvents.Events), "请列出当前货号") {
		t.Fatal("follow-up leaked into the other task")
	}
	if _, err := uc.Get(ctx, 8, created.ID); err == nil {
		t.Fatal("another shop could read the task")
	}
	_ = repo
}

func TestPolicySwitchAdvancesWaitingStageOnce(t *testing.T) {
	uc, repo, _ := testUC(t, NewMockRunner())
	ctx := context.Background()
	seed := &Task{
		ShopID: 7, MasterSKU: "30", Status: StatusPendingText, Phase: PhaseTextReview,
		CodexSessionID: "sess-seed", Revision: 4, ZipLocalPath: t.TempDir(),
		TextDraft: []byte(`{"title_ru":"Товар"}`), SubSKUs: []SubSKU{{OfferID: "30-1"}},
	}
	created, err := repo.Create(ctx, seed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := DecidePolicy(StatusPendingText, false, false, false, false, true, false); !d.AdvanceText || d.AdvanceVisual {
		t.Fatalf("decision = %+v", d)
	}
	advanced, err := uc.UpdatePolicy(ctx, 7, created.ID, created.Revision, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Status != StatusPendingVisual || !advanced.TextApproved || !advanced.TextApprovedAuto || advanced.VisualApproved {
		t.Fatalf("after text auto = %+v", advanced)
	}
	again, err := uc.UpdatePolicy(ctx, 7, created.ID, advanced.Revision, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != StatusPendingVisual || again.Revision != advanced.Revision {
		t.Fatalf("repeat toggle moved the task: %+v", again)
	}
	if _, err := uc.Approve(ctx, 7, created.ID, "text", created.Revision); !conflicted(err) {
		t.Fatalf("stale text approval err = %v", err)
	}
	done, err := uc.UpdatePolicy(ctx, 7, created.ID, again.Revision, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusCompleted || !done.VisualApprovedAuto {
		t.Fatalf("visual auto = %+v", done)
	}
	events, err := uc.Events(ctx, 7, created.ID, EventQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	textPasses := 0
	for _, event := range events.Events {
		if event.Role == RoleReview && strings.Contains(event.Content, "文案审核") {
			textPasses++
		}
	}
	if textPasses != 1 {
		t.Fatalf("text stage passed %d times: %s", textPasses, joinEventText(events.Events))
	}
}

func TestBothSwitchesOnCompleteWhenTextStageIsReached(t *testing.T) {
	uc, repo, _ := testUC(t, NewMockRunner())
	ctx := context.Background()
	created, err := repo.Create(ctx, &Task{
		ShopID: 7, MasterSKU: "30", Status: StatusPendingText, Phase: PhaseTextReview,
		CodexSessionID: "sess-both", Revision: 2, ZipLocalPath: t.TempDir(),
		TextDraft: []byte(`{"title_ru":"Товар"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	done, err := uc.UpdatePolicy(ctx, 7, created.ID, created.Revision, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusCompleted || !done.TextApprovedAuto || !done.VisualApprovedAuto {
		t.Fatalf("both switches = %+v", done)
	}
}

func TestConcurrentApproveConflicts(t *testing.T) {
	uc, repo, _ := testUC(t, NewMockRunner())
	ctx := context.Background()
	created, err := repo.Create(ctx, &Task{
		ShopID: 7, MasterSKU: "30", Status: StatusPendingText, Phase: PhaseTextReview,
		CodexSessionID: "sess-race", Revision: 1, ZipLocalPath: t.TempDir(),
		TextDraft: []byte(`{"title_ru":"Товар"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = uc.Approve(ctx, 7, created.ID, "text", created.Revision)
		}(i)
	}
	close(start)
	wg.Wait()
	wins, conflicts := 0, 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if conflicted(err) {
			conflicts++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d errs=%v", wins, conflicts, errs)
	}
	latest, err := uc.Get(ctx, 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !latest.TextApproved || latest.Status != StatusPendingVisual {
		t.Fatalf("after race = %+v", latest.TaskView)
	}
	events, err := uc.Events(ctx, 7, created.ID, EventQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	passes := 0
	for _, event := range events.Events {
		if strings.Contains(event.Content, "文案审核已人工通过") {
			passes++
		}
	}
	if passes != 1 {
		t.Fatalf("passes = %d\n%s", passes, joinEventText(events.Events))
	}
}

func TestExtractionFailureMarksTaskAndDeletesZip(t *testing.T) {
	uc, _, root := testUC(t, NewMockRunner())
	ctx := context.Background()
	zipPath := filepath.Join(root, "incoming", "bad.zip")
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("ok"))
	w, err = zw.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("nope"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	created, err := uc.Create(ctx, CreateInput{
		ShopID: 7, MasterSKU: "30", DescriptionCategoryID: 17028922, TypeID: 97062518,
		ZipPath: zipPath, ZipFilename: "bad.zip",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusFailed || !strings.Contains(created.ErrorMessage, "不安全路径") {
		t.Fatalf("failed task = %+v", created.TaskView)
	}
	if created.ZipLocalPath != "" {
		t.Fatalf("failed task kept a directory: %s", created.ZipLocalPath)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatal("zip should be deleted after a failed extract")
	}
	if _, statErr := os.Stat(filepath.Join(root, "tasks", "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatal("zip slip wrote outside the task directory")
	}
}

func TestRedactBeforeTimelineWrite(t *testing.T) {
	uc, _, root := testUC(t, leakRunner{})
	ctx := context.Background()
	zipPath := filepath.Join(root, "incoming", "secret.zip")
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSampleZip(t, zipPath, map[string]string{"a.txt": "a"})
	created, err := uc.Create(ctx, CreateInput{
		ShopID: 7, MasterSKU: "30", DescriptionCategoryID: 17028922, TypeID: 97062518,
		ZipPath: zipPath, ZipFilename: "secret.zip",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := uc.Events(ctx, 7, created.ID, EventQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	text := joinEventText(events.Events)
	if strings.Contains(text, "supersecret") {
		t.Fatalf("secret stored in timeline: %s", text)
	}
	if !strings.Contains(text, "Api-Key: ***") {
		t.Fatalf("redaction marker missing: %s", text)
	}
}

type leakRunner struct{}

func (leakRunner) Run(context.Context, RunRequest) (*RunResult, error) {
	return &RunResult{SessionID: "sess-leak", Messages: []RunnerMessage{
		{Role: RoleToolResult, Stage: "解析", ToolCallID: "c1", Content: "Api-Key: supersecret"},
		{Role: RoleAssistant, Stage: "解析", Content: "需要品牌。\n" + SignalFence(map[string]any{
			"signal":         "need_input",
			"missing_fields": []map[string]any{{"key": "brand", "label": "品牌", "required": true}},
		})},
	}}, nil
}

func TestDecidePolicy(t *testing.T) {
	if d := DecidePolicy(StatusPendingText, false, false, false, true, true, true); !d.AdvanceText || d.AdvanceVisual {
		t.Fatalf("text waiting should not also complete visual here: %+v", d)
	}
	if d := DecidePolicy(StatusPendingVisual, true, false, true, false, true, true); d.AdvanceText || !d.AdvanceVisual {
		t.Fatalf("visual waiting = %+v", d)
	}
	if d := DecidePolicy(StatusCompleted, true, true, false, false, true, true); d.AdvanceText || d.AdvanceVisual {
		t.Fatalf("completed task moved: %+v", d)
	}
	if d := DecidePolicy(StatusPendingText, false, false, true, false, true, false); d.AdvanceText {
		t.Fatalf("already-on switch should not pass again: %+v", d)
	}
}

func conflicted(err error) bool {
	be, ok := err.(*Error)
	return ok && be != nil && be.Reason == "REVISION_CONFLICT"
}

func joinEventText(events []TaskEventView) string {
	var b strings.Builder
	for _, event := range events {
		b.WriteString(event.Role)
		b.WriteString(":")
		b.WriteString(event.Content)
		b.WriteString("\n")
	}
	return b.String()
}
