package app

import (
	"context"
	"encoding/json"
	"time"

	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/store"
)

type ReviewDefaults struct {
	Copy         bool `json:"copy"`
	VisualPlan   bool `json:"visual_plan"`
	VisualResult bool `json:"visual_result"`
}

type Settings struct {
	MaxActiveTasks      int                  `json:"max_active_tasks"`
	ActiveCount         int                  `json:"active_count"`
	QueuedCount         int                  `json:"queued_count"`
	PromptFallback      string               `json:"prompt_fallback"`
	GenericPrompt       string               `json:"generic_prompt"`
	ReviewDefaults      ReviewDefaults       `json:"review_defaults"`
	MaxInfraRetries     int                  `json:"max_infra_retries"`
	CodexBinary         string               `json:"codex_binary"`
	PricingRules        []domain.PricingRule `json:"pricing_rules"`
	ActivePricingRuleID int64                `json:"active_pricing_rule_id,omitempty"`
	OSSProvider         string               `json:"oss_provider"`
	OSSConfigured       bool                 `json:"oss_configured"`
	OSSMessage          string               `json:"oss_message"`
}

func (e *Engine) GetSettings(ctx context.Context) (Settings, error) {
	var out Settings
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		out.MaxActiveTasks, err = tx.GetMaxActive(ctx)
		if err != nil {
			return err
		}
		out.ActiveCount, err = tx.CountTasks(ctx, domain.ActiveStatuses())
		if err != nil {
			return err
		}
		out.QueuedCount, err = tx.CountTasks(ctx, []string{domain.StatusQueued})
		if err != nil {
			return err
		}
		out.PromptFallback = configString(ctx, tx, "prompt_fallback", "block")
		out.GenericPrompt = configString(ctx, tx, "generic_prompt", "")
		out.ReviewDefaults = configReviews(ctx, tx)
		out.MaxInfraRetries = configInt(ctx, tx, "max_infra_retries", 0)
		out.CodexBinary = configString(ctx, tx, "codex_binary", "codex")
		out.ActivePricingRuleID = int64(configInt(ctx, tx, "active_pricing_rule_id", 0))
		out.PricingRules, err = tx.ListPricingRules(ctx)
		return err
	})
	if err != nil {
		return out, err
	}
	if out.PricingRules == nil {
		out.PricingRules = []domain.PricingRule{}
	}
	uploader := e.uploader()
	out.OSSProvider = uploader.Name()
	out.OSSConfigured = uploader.Configured()
	out.OSSMessage = uploader.Message()
	return out, nil
}

type SettingsPatch struct {
	MaxActiveTasks      *int
	PromptFallback      *string
	GenericPrompt       *string
	ReviewDefaults      *ReviewDefaults
	MaxInfraRetries     *int
	CodexBinary         *string
	ActivePricingRuleID *int64
}

func (e *Engine) PatchSettings(ctx context.Context, patch SettingsPatch) (Settings, error) {
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		now := e.now()
		if patch.MaxActiveTasks != nil {
			if err := tx.SetMaxActive(ctx, *patch.MaxActiveTasks, now); err != nil {
				return fail(400, "MAX_ACTIVE_TASKS", err.Error(), nil)
			}
		}
		if patch.PromptFallback != nil {
			if *patch.PromptFallback != "block" && *patch.PromptFallback != "generic" {
				return fail(400, "PROMPT_FALLBACK", "prompt_fallback 只能是 block 或 generic", nil)
			}
			if err := putJSON(ctx, tx, "prompt_fallback", *patch.PromptFallback, now); err != nil {
				return err
			}
		}
		if patch.GenericPrompt != nil {
			if err := putJSON(ctx, tx, "generic_prompt", *patch.GenericPrompt, now); err != nil {
				return err
			}
		}
		if patch.ReviewDefaults != nil {
			if err := putJSON(ctx, tx, "review_defaults", *patch.ReviewDefaults, now); err != nil {
				return err
			}
		}
		if patch.MaxInfraRetries != nil {
			if *patch.MaxInfraRetries < 0 {
				return fail(400, "RETRIES", "失败重试上限不能是负数", nil)
			}
			if err := putJSON(ctx, tx, "max_infra_retries", *patch.MaxInfraRetries, now); err != nil {
				return err
			}
		}
		if patch.CodexBinary != nil && *patch.CodexBinary != "" {
			if err := putJSON(ctx, tx, "codex_binary", *patch.CodexBinary, now); err != nil {
				return err
			}
		}
		if patch.ActivePricingRuleID != nil {
			if err := putJSON(ctx, tx, "active_pricing_rule_id", *patch.ActivePricingRuleID, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return e.GetSettings(ctx)
}

func (e *Engine) SavePricingRule(ctx context.Context, name string, enabled bool, params json.RawMessage) (domain.PricingRule, error) {
	if name == "" || len(params) == 0 {
		return domain.PricingRule{}, fail(400, "PRICING_RULE", "定价规则需要名称和参数", nil)
	}
	var probe map[string]any
	if err := json.Unmarshal(params, &probe); err != nil {
		return domain.PricingRule{}, fail(400, "PRICING_RULE", "参数必须是 JSON 对象", nil)
	}
	now := e.now()
	var saved domain.PricingRule
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		saved, err = tx.CreatePricingRule(ctx, domain.PricingRule{Name: name, Enabled: enabled, ParametersJSON: params, CreatedAt: now, UpdatedAt: now})
		return err
	})
	return saved, err
}

func configString(ctx context.Context, tx store.Tx, key, fallback string) string {
	raw, ok, err := tx.GetConfig(ctx, key)
	if err != nil || !ok {
		return fallback
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func configInt(ctx context.Context, tx store.Tx, key string, fallback int) int {
	raw, ok, err := tx.GetConfig(ctx, key)
	if err != nil || !ok {
		return fallback
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}

func configReviews(ctx context.Context, tx store.Tx) ReviewDefaults {
	raw, ok, err := tx.GetConfig(ctx, "review_defaults")
	if err != nil || !ok {
		return ReviewDefaults{}
	}
	var value ReviewDefaults
	if err := json.Unmarshal(raw, &value); err != nil {
		return ReviewDefaults{}
	}
	return value
}

func putJSON(ctx context.Context, tx store.Tx, key string, value any, now time.Time) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return tx.SetConfig(ctx, key, raw, now)
}
