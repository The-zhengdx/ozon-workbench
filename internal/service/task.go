package service

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/conf"
)

// TaskService is the HTTP adapter for the material-processing workbench.
type TaskService struct {
	uc     *biz.TaskUsecase
	maxZip int64
	log    *log.Helper
}

func NewTaskService(uc *biz.TaskUsecase, cfg *conf.Runtime, logger log.Logger) *TaskService {
	maxZip := cfg.MaxZipBytes
	if maxZip <= 0 {
		maxZip = 200 << 20
	}
	return &TaskService{uc: uc, maxZip: maxZip, log: log.NewHelper(logger)}
}

func (s *TaskService) Register(r *khttp.Router) {
	r.GET("/tasks", s.list)
	r.POST("/tasks", s.create)
	r.GET("/tasks/{id}", s.get)
	r.POST("/tasks/{id}/policy", s.policy)
	r.POST("/tasks/{id}/approve", s.approve)
	r.POST("/tasks/{id}/input", s.input)
	r.POST("/tasks/{id}/messages", s.message)
	r.GET("/tasks/{id}/events", s.events)
}

func (s *TaskService) list(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	page, err := s.uc.List(ctx, shopID, ctx.Query().Get("status"), queryInt(ctx, "page"), queryInt(ctx, "page_size"))
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, page)
}

func (s *TaskService) get(ctx khttp.Context) error {
	shopID, id, err := shopAndTask(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	detail, err := s.uc.Get(ctx, shopID, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, detail)
}

func (s *TaskService) create(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	req := ctx.Request()
	req.Body = http.MaxBytesReader(ctx.Response(), req.Body, s.maxZip+1<<20)
	reader, err := req.MultipartReader()
	if err != nil {
		return writeErr(ctx, s.log, &biz.Error{Status: 400, Reason: "BAD_MULTIPART", Message: "请用 multipart 上传素材包。"})
	}
	in := biz.CreateInput{ShopID: shopID}
	var zipPath string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.removeUpload(zipPath)
			return writeErr(ctx, s.log, uploadReadErr(err))
		}
		filename := part.FileName()
		formName := part.FormName()
		if formName == "file" {
			if filename == "" {
				part.Close()
				s.removeUpload(zipPath)
				return writeErr(ctx, s.log, &biz.Error{Status: 400, Reason: "ZIP_REQUIRED", Message: "请上传 1688 素材压缩包。", Fields: map[string]string{"file": "请上传 1688 素材压缩包。"}})
			}
			path, err := s.uc.SaveUpload(part, filename)
			part.Close()
			if err != nil {
				s.removeUpload(zipPath)
				return writeErr(ctx, s.log, uploadReadErr(err))
			}
			s.removeUpload(zipPath)
			zipPath = path
			in.ZipPath = path
			in.ZipFilename = filename
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(part, 1<<20))
		part.Close()
		if err != nil {
			s.removeUpload(zipPath)
			return writeErr(ctx, s.log, uploadReadErr(err))
		}
		value := strings.TrimSpace(string(raw))
		switch formName {
		case "master_sku":
			in.MasterSKU = value
		case "supplier_url":
			in.SupplierURL = value
		case "description_category_id":
			in.DescriptionCategoryID, err = parsePositive(value, "description_category_id")
		case "type_id":
			in.TypeID, err = parsePositive(value, "type_id")
		case "language":
			in.Language = value
		case "auto_text_approve":
			in.AutoText = formBool(value)
		case "auto_visual_approve":
			in.AutoVisual = formBool(value)
		}
		if err != nil {
			s.removeUpload(zipPath)
			return writeErr(ctx, s.log, err)
		}
	}
	detail, err := s.uc.Create(ctx, in)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusCreated, detail)
}

func (s *TaskService) policy(ctx khttp.Context) error {
	shopID, id, err := shopAndTask(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Revision          int64 `json:"revision"`
		AutoTextApprove   bool  `json:"auto_text_approve"`
		AutoVisualApprove bool  `json:"auto_visual_approve"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.UpdatePolicy(ctx, shopID, id, body.Revision, body.AutoTextApprove, body.AutoVisualApprove)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *TaskService) approve(ctx khttp.Context) error {
	shopID, id, err := shopAndTask(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Revision int64  `json:"revision"`
		Stage    string `json:"stage"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.Approve(ctx, shopID, id, body.Stage, body.Revision)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *TaskService) input(ctx khttp.Context) error {
	shopID, id, err := shopAndTask(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Revision int64             `json:"revision"`
		Fields   map[string]string `json:"fields"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.SubmitInput(ctx, shopID, id, body.Revision, body.Fields)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *TaskService) message(ctx khttp.Context) error {
	shopID, id, err := shopAndTask(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Revision int64  `json:"revision"`
		Content  string `json:"content"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.FollowUp(ctx, shopID, id, body.Revision, body.Content)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *TaskService) events(ctx khttp.Context) error {
	shopID, id, err := shopAndTask(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	page, err := s.uc.Events(ctx, shopID, id, biz.EventQuery{
		AfterSeq:  queryInt(ctx, "after_seq"),
		BeforeSeq: queryInt(ctx, "before_seq"),
		Limit:     queryInt(ctx, "limit"),
	})
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, page)
}

// ServeMedia sends one extracted file. The path is /media/tasks/{id}/{file}.
func (s *TaskService) ServeMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/media/tasks/")
	idStr, file, ok := strings.Cut(rel, "/")
	id, err := strconv.Atoi(idStr)
	if !ok || err != nil || id <= 0 || file == "" {
		http.NotFound(w, r)
		return
	}
	full, err := s.uc.MediaPath(r.Context(), id, file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

func (s *TaskService) removeUpload(path string) {
	if path == "" {
		return
	}
	_ = s.uc.DiscardUpload(path)
}

func shopAndTask(ctx khttp.Context) (int, int, error) {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return 0, 0, err
	}
	id, err := taskPathID(ctx)
	if err != nil {
		return 0, 0, err
	}
	return shopID, id, nil
}

func taskPathID(ctx khttp.Context) (int, error) {
	id, err := strconv.Atoi(ctx.Vars().Get("id"))
	if err != nil || id <= 0 {
		return 0, &biz.Error{Status: 400, Reason: "BAD_ID", Message: "任务 ID 不正确。"}
	}
	return id, nil
}

func parsePositive(raw, name string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, &biz.Error{Status: 400, Reason: "BAD_TYPE", Message: "请选择可用的末级类型。", Fields: map[string]string{"type_id": "请选择可用的末级类型。"}}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0, &biz.Error{Status: 400, Reason: "BAD_TYPE", Message: "参数 " + name + " 不正确。", Fields: map[string]string{name: "参数不正确。"}}
	}
	return n, nil
}

func formBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func uploadReadErr(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || strings.Contains(err.Error(), "too large") {
		return &biz.Error{Status: 400, Reason: "ZIP_TOO_LARGE", Message: "压缩包超过大小限制。", Fields: map[string]string{"file": "压缩包超过大小限制。"}}
	}
	if be, ok := err.(*biz.Error); ok {
		return be
	}
	return err
}
