package biz

import "context"

const (
	StageExtract  = "extract"
	StageResume   = "resume"
	StageVisual   = "visual"
	StageFollowUp = "follow_up"
	StageListing  = "listing"
)

// ListingFacts is the confirmed product data Codex turns into one import item.
type ListingFacts struct {
	OfferID               string
	Specs                 string
	Title                 string
	Currency              string
	Price                 string
	PrimaryImage          string
	Images                []string
	DescriptionCategoryID int64
	TypeID                int64
	Attributes            []map[string]any
	Answers               map[string]string
}

// RunRequest is one turn sent to Codex. SessionID is empty for the first turn.
type RunRequest struct {
	Stage      string
	SessionID  string
	Prompt     string
	MasterSKU  string
	WorkDir    string
	Answers    map[string]string
	Attributes []map[string]any
	Listing    *ListingFacts
}

// RunnerMessage is one timeline line produced by a runner before redaction.
type RunnerMessage struct {
	Role       string
	Stage      string
	Content    string
	ToolCallID string
}

// RunResult is the outcome of one Codex turn. Messages keep tool calls paired by ToolCallID.
type RunResult struct {
	SessionID string
	Messages  []RunnerMessage
}

// AssistantText joins model-visible replies so the signal parser sees the whole turn.
func (r *RunResult) AssistantText() string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, msg := range r.Messages {
		if msg.Role == RoleAssistant {
			parts = append(parts, msg.Content)
		}
	}
	return joinLines(parts)
}

func joinLines(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += "\n"
		}
		out += part
	}
	return out
}

// CodexRunner executes one non-interactive Codex turn.
// The mock runner is the default in development. The CLI runner shells out to `codex exec`.
type CodexRunner interface {
	Run(ctx context.Context, req RunRequest) (*RunResult, error)
}
