package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ozon-workbench/internal/domain"
)

type PG struct{ pool *pgxpool.Pool }

func OpenPostgres(ctx context.Context, url string) (*PG, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PG{pool: pool}, nil
}

func (p *PG) Close() { p.pool.Close() }

func (p *PG) Pool() *pgxpool.Pool { return p.pool }

func (p *PG) InTx(ctx context.Context, fn func(Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(&pgTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type pgTx struct{ tx pgx.Tx }

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func ns(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nraw(v json.RawMessage) any {
	if len(v) == 0 {
		return nil
	}
	return []byte(v)
}

func jarr(v json.RawMessage) any {
	if len(v) == 0 {
		return []byte("[]")
	}
	return []byte(v)
}

func jobj(v json.RawMessage) any {
	if len(v) == 0 {
		return []byte("{}")
	}
	return []byte(v)
}

func (t *pgTx) CreateShop(ctx context.Context, shop domain.Shop) (domain.Shop, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_shops (name, client_id, api_key, currency_code, vat, enable, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8) RETURNING id`,
		shop.Name, shop.ClientID, shop.APIKey, shop.CurrencyCode, shop.VAT, shop.Enable, shop.CreatedAt, shop.UpdatedAt,
	).Scan(&shop.ID)
	if isUnique(err) {
		return domain.Shop{}, ErrConflict
	}
	return shop, err
}

func (t *pgTx) UpdateShop(ctx context.Context, shop domain.Shop) (domain.Shop, error) {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_shops SET name=$2, client_id=$3, api_key=$4, currency_code=$5, vat=$6::numeric, enable=$7, updated_at=$8
		WHERE id=$1`,
		shop.ID, shop.Name, shop.ClientID, shop.APIKey, shop.CurrencyCode, shop.VAT, shop.Enable, shop.UpdatedAt)
	if isUnique(err) {
		return domain.Shop{}, ErrConflict
	}
	if err != nil {
		return domain.Shop{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Shop{}, ErrNotFound
	}
	return shop, nil
}

func (t *pgTx) ListShops(ctx context.Context) ([]domain.Shop, error) {
	rows, err := t.tx.Query(ctx, shopSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Shop
	for rows.Next() {
		shop, err := scanShop(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, shop)
	}
	return out, rows.Err()
}

func (t *pgTx) GetShop(ctx context.Context, id int64) (domain.Shop, error) {
	rows, err := t.tx.Query(ctx, shopSelect+` WHERE id=$1`, id)
	if err != nil {
		return domain.Shop{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Shop{}, ErrNotFound
	}
	return scanShop(rows)
}

const shopSelect = `SELECT id, name, client_id, api_key, currency_code, vat::text, enable, created_at, updated_at FROM ozon_shops`

func scanShop(row pgx.Row) (domain.Shop, error) {
	var shop domain.Shop
	err := row.Scan(&shop.ID, &shop.Name, &shop.ClientID, &shop.APIKey, &shop.CurrencyCode, &shop.VAT, &shop.Enable, &shop.CreatedAt, &shop.UpdatedAt)
	return shop, err
}

func (t *pgTx) GetMaxActive(ctx context.Context) (int, error) {
	var n int
	err := t.tx.QueryRow(ctx, `SELECT max_active_tasks FROM ozon_system_settings WHERE id=1`).Scan(&n)
	return n, err
}

func (t *pgTx) SetMaxActive(ctx context.Context, n int, now time.Time) error {
	if n <= 0 {
		return fmt.Errorf("max_active_tasks 必须是正整数")
	}
	_, err := t.tx.Exec(ctx, `UPDATE ozon_system_settings SET max_active_tasks=$1, updated_at=$2 WHERE id=1`, n, now)
	return err
}

func (t *pgTx) GetConfig(ctx context.Context, key string) (json.RawMessage, bool, error) {
	var raw []byte
	err := t.tx.QueryRow(ctx, `SELECT value_json FROM ozon_app_config WHERE key=$1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return raw, err == nil, err
}

func (t *pgTx) SetConfig(ctx context.Context, key string, val json.RawMessage, now time.Time) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ozon_app_config(key, value_json, updated_at) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value_json=EXCLUDED.value_json, updated_at=EXCLUDED.updated_at`,
		key, []byte(val), now)
	return err
}

func (t *pgTx) PutTree(ctx context.Context, lang string, tree json.RawMessage, synced time.Time) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ozon_category_tree_cache(language, tree_json, synced_at) VALUES ($1,$2,$3)
		ON CONFLICT (language) DO UPDATE SET tree_json=EXCLUDED.tree_json, synced_at=EXCLUDED.synced_at`,
		lang, []byte(tree), synced)
	return err
}

func (t *pgTx) GetTree(ctx context.Context, lang string) (json.RawMessage, time.Time, bool, error) {
	var raw []byte
	var synced time.Time
	err := t.tx.QueryRow(ctx, `SELECT tree_json, synced_at FROM ozon_category_tree_cache WHERE language=$1`, lang).Scan(&raw, &synced)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, false, nil
	}
	return raw, synced, err == nil, err
}

func (t *pgTx) GetRule(ctx context.Context, categoryID, typeID int64) (domain.Rule, bool, error) {
	var rule domain.Rule
	err := t.tx.QueryRow(ctx, `
		SELECT id, description_category_id, type_id, prompt, enabled, version, created_at, updated_at
		FROM ozon_category_ai_rules WHERE description_category_id=$1 AND type_id=$2`, categoryID, typeID).
		Scan(&rule.ID, &rule.DescriptionCategoryID, &rule.TypeID, &rule.Prompt, &rule.Enabled, &rule.Version, &rule.CreatedAt, &rule.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Rule{}, false, nil
	}
	return rule, err == nil, err
}

func (t *pgTx) SaveRule(ctx context.Context, rule domain.Rule, now time.Time) (domain.Rule, error) {
	existing, ok, err := t.GetRule(ctx, rule.DescriptionCategoryID, rule.TypeID)
	if err != nil {
		return domain.Rule{}, err
	}
	if !ok {
		err = t.tx.QueryRow(ctx, `
			INSERT INTO ozon_category_ai_rules(description_category_id, type_id, prompt, enabled, version, created_at, updated_at)
			VALUES ($1,$2,$3,$4,1,$5,$5) RETURNING id`,
			rule.DescriptionCategoryID, rule.TypeID, rule.Prompt, rule.Enabled, now).Scan(&rule.ID)
		if err != nil {
			return domain.Rule{}, err
		}
		rule.Version = 1
		rule.CreatedAt = now
		rule.UpdatedAt = now
		return rule, nil
	}
	existing.Prompt = rule.Prompt
	existing.Enabled = rule.Enabled
	existing.Version++
	existing.UpdatedAt = now
	_, err = t.tx.Exec(ctx, `UPDATE ozon_category_ai_rules SET prompt=$2, enabled=$3, version=$4, updated_at=$5 WHERE id=$1`,
		existing.ID, existing.Prompt, existing.Enabled, existing.Version, now)
	return existing, err
}

func (t *pgTx) GetAttrCache(ctx context.Context, shopID, categoryID, typeID int64, lang string) (json.RawMessage, time.Time, *time.Time, bool, error) {
	var raw []byte
	var fetched time.Time
	var expires *time.Time
	err := t.tx.QueryRow(ctx, `
		SELECT attributes_json, fetched_at, expires_at FROM ozon_category_attribute_cache
		WHERE shop_id=$1 AND description_category_id=$2 AND type_id=$3 AND language=$4`,
		shopID, categoryID, typeID, lang).Scan(&raw, &fetched, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, nil, false, nil
	}
	return raw, fetched, expires, err == nil, err
}

func (t *pgTx) PutAttrCache(ctx context.Context, shopID, categoryID, typeID int64, lang string, attrs json.RawMessage, fetched time.Time, expires *time.Time) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ozon_category_attribute_cache(shop_id, description_category_id, type_id, language, attributes_json, fetched_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (shop_id, description_category_id, type_id, language)
		DO UPDATE SET attributes_json=EXCLUDED.attributes_json, fetched_at=EXCLUDED.fetched_at, expires_at=EXCLUDED.expires_at`,
		shopID, categoryID, typeID, lang, []byte(attrs), fetched, expires)
	return err
}

func (t *pgTx) ListDict(ctx context.Context, q domain.DictQuery) ([]domain.DictValue, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100000
	}
	keyword := strings.TrimSpace(q.Keyword)
	rows, err := t.tx.Query(ctx, `
		SELECT shop_id, description_category_id, type_id, attribute_id, language, value_id, value_text, COALESCE(info,''), COALESCE(picture,''), source, fetched_at
		FROM ozon_attribute_dictionary_values
		WHERE shop_id=$1 AND description_category_id=$2 AND type_id=$3 AND attribute_id=$4 AND language=$5
		  AND ($6='' OR lower(value_text) LIKE '%'||lower($6)||'%' OR value_id::text = $6)
		ORDER BY value_id OFFSET $7 LIMIT $8`,
		q.ShopID, q.CategoryID, q.TypeID, q.AttributeID, q.Language, keyword, q.Offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DictValue
	for rows.Next() {
		var row domain.DictValue
		if err := rows.Scan(&row.ShopID, &row.DescriptionCategoryID, &row.TypeID, &row.AttributeID, &row.Language, &row.ValueID, &row.ValueText, &row.Info, &row.Picture, &row.Source, &row.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (t *pgTx) UpsertDict(ctx context.Context, rows []domain.DictValue) error {
	for _, row := range rows {
		_, err := t.tx.Exec(ctx, `
			INSERT INTO ozon_attribute_dictionary_values(
				shop_id, description_category_id, type_id, attribute_id, language, value_id, value_text, info, picture, source, fetched_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (shop_id, description_category_id, type_id, attribute_id, language, value_id)
			DO UPDATE SET value_text=EXCLUDED.value_text, info=EXCLUDED.info, picture=EXCLUDED.picture, source=EXCLUDED.source, fetched_at=EXCLUDED.fetched_at`,
			row.ShopID, row.DescriptionCategoryID, row.TypeID, row.AttributeID, row.Language, row.ValueID, row.ValueText, ns(row.Info), ns(row.Picture), row.Source, row.FetchedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *pgTx) GetDictSync(ctx context.Context, shopID, categoryID, typeID, attributeID int64, lang string) (domain.DictSync, bool, error) {
	var row domain.DictSync
	err := t.tx.QueryRow(ctx, `
		SELECT shop_id, description_category_id, type_id, attribute_id, language, list_status, last_value_id, synced_at, expires_at
		FROM ozon_attribute_dictionary_sync
		WHERE shop_id=$1 AND description_category_id=$2 AND type_id=$3 AND attribute_id=$4 AND language=$5`,
		shopID, categoryID, typeID, attributeID, lang).
		Scan(&row.ShopID, &row.DescriptionCategoryID, &row.TypeID, &row.AttributeID, &row.Language, &row.ListStatus, &row.LastValueID, &row.SyncedAt, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DictSync{}, false, nil
	}
	return row, err == nil, err
}

func (t *pgTx) PutDictSync(ctx context.Context, row domain.DictSync) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ozon_attribute_dictionary_sync(
			shop_id, description_category_id, type_id, attribute_id, language, list_status, last_value_id, synced_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (shop_id, description_category_id, type_id, attribute_id, language)
		DO UPDATE SET list_status=EXCLUDED.list_status, last_value_id=EXCLUDED.last_value_id, synced_at=EXCLUDED.synced_at, expires_at=EXCLUDED.expires_at`,
		row.ShopID, row.DescriptionCategoryID, row.TypeID, row.AttributeID, row.Language, row.ListStatus, row.LastValueID, row.SyncedAt, row.ExpiresAt)
	return err
}

func (t *pgTx) CreateTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_processing_tasks(
			shop_id, description_category_id, type_id, base_sku, origin_link, category_rule_id, category_rule_version,
			prompt_snapshot, codex_session_id, category_token, status, stage, requirement_revision, applied_requirement_revision,
			copy_auto_approve, visual_plan_auto_approve, visual_result_auto_approve, copy_approved, visual_plan_approved, visual_result_approved,
			last_reply_json, last_error, admitted_at, finished_at, cancel_requested, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
		RETURNING id`,
		task.ShopID, task.DescriptionCategoryID, task.TypeID, task.BaseSKU, ns(task.OriginLink), task.CategoryRuleID, task.CategoryRuleVersion,
		task.PromptSnapshot, ns(task.CodexSessionID), ns(task.CategoryToken), task.Status, task.Stage, task.RequirementRevision, task.AppliedRequirementRevision,
		task.CopyAutoApprove, task.VisualPlanAutoApprove, task.VisualResultAutoApprove, task.CopyApproved, task.VisualPlanApproved, task.VisualResultApproved,
		nraw(task.LastReplyJSON), ns(task.LastError), task.AdmittedAt, task.FinishedAt, task.CancelRequested, task.CreatedAt, task.UpdatedAt,
	).Scan(&task.ID)
	if isUnique(err) {
		return domain.Task{}, ErrConflict
	}
	return task, err
}

func (t *pgTx) UpdateTask(ctx context.Context, task domain.Task) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_processing_tasks SET
			shop_id=$2, description_category_id=$3, type_id=$4, base_sku=$5, origin_link=$6, category_rule_id=$7, category_rule_version=$8,
			prompt_snapshot=$9, codex_session_id=$10, category_token=$11, status=$12, stage=$13, requirement_revision=$14, applied_requirement_revision=$15,
			copy_auto_approve=$16, visual_plan_auto_approve=$17, visual_result_auto_approve=$18, copy_approved=$19, visual_plan_approved=$20, visual_result_approved=$21,
			last_reply_json=$22, last_error=$23, admitted_at=$24, finished_at=$25, cancel_requested=$26, updated_at=$27
		WHERE id=$1`,
		task.ID, task.ShopID, task.DescriptionCategoryID, task.TypeID, task.BaseSKU, ns(task.OriginLink), task.CategoryRuleID, task.CategoryRuleVersion,
		task.PromptSnapshot, ns(task.CodexSessionID), ns(task.CategoryToken), task.Status, task.Stage, task.RequirementRevision, task.AppliedRequirementRevision,
		task.CopyAutoApprove, task.VisualPlanAutoApprove, task.VisualResultAutoApprove, task.CopyApproved, task.VisualPlanApproved, task.VisualResultApproved,
		nraw(task.LastReplyJSON), ns(task.LastError), task.AdmittedAt, task.FinishedAt, task.CancelRequested, task.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanTask(row pgx.Row) (domain.Task, error) {
	var task domain.Task
	var origin, session, token, lastErr *string
	var reply []byte
	err := row.Scan(
		&task.ID, &task.ShopID, &task.DescriptionCategoryID, &task.TypeID, &task.BaseSKU, &origin, &task.CategoryRuleID, &task.CategoryRuleVersion,
		&task.PromptSnapshot, &session, &token, &task.Status, &task.Stage, &task.RequirementRevision, &task.AppliedRequirementRevision,
		&task.CopyAutoApprove, &task.VisualPlanAutoApprove, &task.VisualResultAutoApprove, &task.CopyApproved, &task.VisualPlanApproved, &task.VisualResultApproved,
		&reply, &lastErr, &task.AdmittedAt, &task.FinishedAt, &task.CancelRequested, &task.CreatedAt, &task.UpdatedAt,
	)
	if origin != nil {
		task.OriginLink = *origin
	}
	if session != nil {
		task.CodexSessionID = *session
	}
	if token != nil {
		task.CategoryToken = *token
	}
	if lastErr != nil {
		task.LastError = *lastErr
	}
	task.LastReplyJSON = reply
	return task, err
}

const taskSelect = `SELECT id, shop_id, description_category_id, type_id, base_sku, origin_link, category_rule_id, category_rule_version,
	prompt_snapshot, codex_session_id, category_token, status, stage, requirement_revision, applied_requirement_revision,
	copy_auto_approve, visual_plan_auto_approve, visual_result_auto_approve, copy_approved, visual_plan_approved, visual_result_approved,
	last_reply_json, last_error, admitted_at, finished_at, cancel_requested, created_at, updated_at FROM ozon_processing_tasks`

func (t *pgTx) GetTaskByToken(ctx context.Context, token string) (domain.Task, error) {
	rows, err := t.tx.Query(ctx, taskSelect+` WHERE category_token=$1`, token)
	if err != nil {
		return domain.Task{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Task{}, ErrNotFound
	}
	return scanTask(rows)
}

func (t *pgTx) GetTask(ctx context.Context, id int64) (domain.Task, error) {
	q := taskSelect + ` WHERE id=$1`
	if wantsLock(ctx) {
		q += ` FOR UPDATE`
	}
	rows, err := t.tx.Query(ctx, q, id)
	if err != nil {
		return domain.Task{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Task{}, ErrNotFound
	}
	return scanTask(rows)
}

func (t *pgTx) ListTasks(ctx context.Context, f domain.TaskFilter) ([]domain.Task, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100000
	}
	rows, err := t.tx.Query(ctx, taskSelect+`
		WHERE ($1=0 OR shop_id=$1) AND ($2='' OR status=$2) AND ($3='' OR lower(base_sku) LIKE '%'||lower($3)||'%')
		ORDER BY created_at DESC, id DESC OFFSET $4 LIMIT $5`, f.ShopID, f.Status, f.BaseSKU, f.Offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (t *pgTx) CountTasks(ctx context.Context, statuses []string) (int, error) {
	var n int
	if len(statuses) == 0 {
		err := t.tx.QueryRow(ctx, `SELECT count(*) FROM ozon_processing_tasks`).Scan(&n)
		return n, err
	}
	err := t.tx.QueryRow(ctx, `SELECT count(*) FROM ozon_processing_tasks WHERE status = ANY($1)`, statuses).Scan(&n)
	return n, err
}

func (t *pgTx) ClaimQueued(ctx context.Context, now time.Time) ([]domain.Task, error) {
	var maxActive int
	if err := t.tx.QueryRow(ctx, `SELECT max_active_tasks FROM ozon_system_settings WHERE id=1 FOR UPDATE`).Scan(&maxActive); err != nil {
		return nil, err
	}
	var active int
	if err := t.tx.QueryRow(ctx, `SELECT count(*) FROM ozon_processing_tasks WHERE status = ANY($1)`, domain.ActiveStatuses()).Scan(&active); err != nil {
		return nil, err
	}
	slots := maxActive - active
	if slots <= 0 {
		return nil, nil
	}
	rows, err := t.tx.Query(ctx, taskSelect+` WHERE status='queued' ORDER BY created_at, id FOR UPDATE SKIP LOCKED LIMIT $1`, slots)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var claimed []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range claimed {
		claimed[i].Status = domain.StatusDispatching
		claimed[i].AdmittedAt = &now
		claimed[i].UpdatedAt = now
		if err := t.UpdateTask(ctx, claimed[i]); err != nil {
			return nil, err
		}
	}
	return claimed, nil
}

func (t *pgTx) CreateTurn(ctx context.Context, turn domain.Turn) (domain.Turn, error) {
	var exists int
	if err := t.tx.QueryRow(ctx, `SELECT count(*) FROM ozon_processing_turns WHERE task_id=$1 AND run_status IN ('starting','running')`, turn.TaskID).Scan(&exists); err != nil {
		return domain.Turn{}, err
	}
	if exists > 0 {
		return domain.Turn{}, ErrConflict
	}
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_processing_turns(task_id, seq, run_status, input_max_requirement_revision, output_file, exit_code, reply_json, error_message, started_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		turn.TaskID, turn.Seq, turn.RunStatus, turn.InputMaxRequirementRevision, ns(turn.OutputFile), turn.ExitCode, nraw(turn.ReplyJSON), ns(turn.ErrorMessage), turn.StartedAt, turn.FinishedAt,
	).Scan(&turn.ID)
	return turn, err
}

func (t *pgTx) UpdateTurn(ctx context.Context, turn domain.Turn) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_processing_turns SET seq=$2, run_status=$3, input_max_requirement_revision=$4, output_file=$5, exit_code=$6, reply_json=$7, error_message=$8, started_at=$9, finished_at=$10
		WHERE id=$1`,
		turn.ID, turn.Seq, turn.RunStatus, turn.InputMaxRequirementRevision, ns(turn.OutputFile), turn.ExitCode, nraw(turn.ReplyJSON), ns(turn.ErrorMessage), turn.StartedAt, turn.FinishedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanTurn(row pgx.Row) (domain.Turn, error) {
	var turn domain.Turn
	var output, errMsg *string
	var reply []byte
	err := row.Scan(&turn.ID, &turn.TaskID, &turn.Seq, &turn.RunStatus, &turn.InputMaxRequirementRevision, &output, &turn.ExitCode, &reply, &errMsg, &turn.StartedAt, &turn.FinishedAt)
	if output != nil {
		turn.OutputFile = *output
	}
	if errMsg != nil {
		turn.ErrorMessage = *errMsg
	}
	turn.ReplyJSON = reply
	return turn, err
}

const turnSelect = `SELECT id, task_id, seq, run_status, input_max_requirement_revision, output_file, exit_code, reply_json, error_message, started_at, finished_at FROM ozon_processing_turns`

func (t *pgTx) ListTurns(ctx context.Context, taskID int64) ([]domain.Turn, error) {
	rows, err := t.tx.Query(ctx, turnSelect+` WHERE task_id=$1 ORDER BY seq`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Turn
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, rows.Err()
}

func (t *pgTx) ActiveTurn(ctx context.Context, taskID int64) (domain.Turn, bool, error) {
	rows, err := t.tx.Query(ctx, turnSelect+` WHERE task_id=$1 AND run_status IN ('starting','running')`, taskID)
	if err != nil {
		return domain.Turn{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Turn{}, false, rows.Err()
	}
	turn, err := scanTurn(rows)
	return turn, err == nil, err
}

func (t *pgTx) CreateMessage(ctx context.Context, msg domain.Message) (domain.Message, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_processing_messages(task_id, turn_id, role, kind, content, payload, requirement_revision, delivery_status, created_at, processed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		msg.TaskID, msg.TurnID, msg.Role, msg.Kind, msg.Content, nraw(msg.Payload), msg.RequirementRevision, msg.DeliveryStatus, msg.CreatedAt, msg.ProcessedAt,
	).Scan(&msg.ID)
	return msg, err
}

func (t *pgTx) UpdateMessage(ctx context.Context, msg domain.Message) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_processing_messages SET task_id=$2, turn_id=$3, role=$4, kind=$5, content=$6, payload=$7, requirement_revision=$8, delivery_status=$9, processed_at=$10
		WHERE id=$1`,
		msg.ID, msg.TaskID, msg.TurnID, msg.Role, msg.Kind, msg.Content, nraw(msg.Payload), msg.RequirementRevision, msg.DeliveryStatus, msg.ProcessedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanMessage(row pgx.Row) (domain.Message, error) {
	var msg domain.Message
	var payload []byte
	err := row.Scan(&msg.ID, &msg.TaskID, &msg.TurnID, &msg.Role, &msg.Kind, &msg.Content, &payload, &msg.RequirementRevision, &msg.DeliveryStatus, &msg.CreatedAt, &msg.ProcessedAt)
	msg.Payload = payload
	return msg, err
}

const messageSelect = `SELECT id, task_id, turn_id, role, kind, content, payload, requirement_revision, delivery_status, created_at, processed_at FROM ozon_processing_messages`

func (t *pgTx) ListMessages(ctx context.Context, taskID int64) ([]domain.Message, error) {
	return t.queryMessages(ctx, messageSelect+` WHERE task_id=$1 ORDER BY id`, taskID)
}

func (t *pgTx) PendingMessages(ctx context.Context, taskID int64) ([]domain.Message, error) {
	return t.queryMessages(ctx, messageSelect+` WHERE task_id=$1 AND delivery_status='pending' AND role IN ('user','system') ORDER BY id`, taskID)
}

func (t *pgTx) queryMessages(ctx context.Context, q string, args ...any) ([]domain.Message, error) {
	rows, err := t.tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (t *pgTx) UpsertAsset(ctx context.Context, asset domain.Asset) (domain.Asset, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_media_assets(task_id, asset_id, sku_ref, role, local_path, object_key, public_url, mime_type, upload_status, error_message, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (task_id, asset_id) DO UPDATE SET
			sku_ref=EXCLUDED.sku_ref, role=EXCLUDED.role, local_path=EXCLUDED.local_path, object_key=EXCLUDED.object_key,
			public_url=EXCLUDED.public_url, mime_type=EXCLUDED.mime_type, upload_status=EXCLUDED.upload_status,
			error_message=EXCLUDED.error_message, updated_at=EXCLUDED.updated_at
		RETURNING id, created_at`,
		asset.TaskID, asset.AssetID, asset.SKURef, asset.Role, asset.LocalPath, ns(asset.ObjectKey), ns(asset.PublicURL), ns(asset.MimeType), asset.UploadStatus, ns(asset.ErrorMessage), asset.CreatedAt, asset.UpdatedAt,
	).Scan(&asset.ID, &asset.CreatedAt)
	return asset, err
}

func (t *pgTx) ListAssets(ctx context.Context, taskID int64) ([]domain.Asset, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id, task_id, asset_id, sku_ref, role, local_path, object_key, public_url, mime_type, upload_status, error_message, created_at, updated_at
		FROM ozon_media_assets WHERE task_id=$1 ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Asset
	for rows.Next() {
		var asset domain.Asset
		var key, url, mime, errMsg *string
		if err := rows.Scan(&asset.ID, &asset.TaskID, &asset.AssetID, &asset.SKURef, &asset.Role, &asset.LocalPath, &key, &url, &mime, &asset.UploadStatus, &errMsg, &asset.CreatedAt, &asset.UpdatedAt); err != nil {
			return nil, err
		}
		asset.ObjectKey = deref(key)
		asset.PublicURL = deref(url)
		asset.MimeType = deref(mime)
		asset.ErrorMessage = deref(errMsg)
		out = append(out, asset)
	}
	return out, rows.Err()
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
