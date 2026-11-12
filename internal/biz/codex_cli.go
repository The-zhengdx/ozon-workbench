package biz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CLIConfig is the non-interactive Codex command.
// NewArgs and ResumeArgs are argv templates. The prompt is written to stdin,
// which the official CLI reads when the prompt argument is "-".
//
// Defaults follow the current docs (https://learn.chatgpt.com/docs/non-interactive-mode):
//
//	codex exec --json --skip-git-repo-check --sandbox workspace-write -C <dir> -o <file> -
//	codex exec resume <SESSION_ID> --json --skip-git-repo-check --sandbox workspace-write -C <dir> -o <file> -
//
// Placeholders: {{work_dir}} {{session_id}} {{last_message}} {{binary}}.
type CLIConfig struct {
	Binary     string
	NewArgs    []string
	ResumeArgs []string
}

// CLIRunner shells out to the local Codex CLI. It is not used when codex.mock is true.
type CLIRunner struct {
	CLIConfig
}

func NewCLIRunner(cfg CLIConfig) *CLIRunner {
	return &CLIRunner{CLIConfig: cfg}
}

func (r *CLIRunner) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	binary := strings.TrimSpace(r.Binary)
	if binary == "" {
		binary = "codex"
	}
	work := req.WorkDir
	if strings.TrimSpace(work) == "" {
		return nil, fmt.Errorf("Codex 工作目录为空")
	}
	resume := req.Stage != StageExtract
	if resume && strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("没有可恢复的 Codex 会话")
	}
	last := filepath.Join(work, ".codex-last-message.txt")
	tmpl := r.NewArgs
	if resume {
		tmpl = r.ResumeArgs
	}
	if len(tmpl) == 0 {
		return nil, fmt.Errorf("未配置 Codex 命令模板")
	}
	args := expandArgs(tmpl, map[string]string{
		"work_dir":     work,
		"session_id":   req.SessionID,
		"last_message": last,
		"binary":       binary,
	})
	bin, err := resolveBinary(binary)
	if err != nil {
		return nil, err
	}
	cmd, err := commandContext(ctx, bin, args)
	if err != nil {
		return nil, err
	}
	cmd.Dir = work
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	result := parseCodexStdout(stdout.Bytes(), last)
	if result.SessionID == "" {
		result.SessionID = req.SessionID
	}
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		msg = truncateRunes(Redact(msg), 2000)
		return result, fmt.Errorf("Codex CLI 执行失败：%s", msg)
	}
	if strings.TrimSpace(result.AssistantText()) == "" {
		return result, fmt.Errorf("Codex 没有返回文本")
	}
	return result, nil
}

func resolveBinary(binary string) (string, error) {
	if runtime.GOOS == "windows" {
		// LookPath applies PATHEXT, so "codex", "codex.cmd", and a path that
		// omits .exe/.cmd all resolve. os.Stat would miss the npm shim codex.cmd.
		path, err := exec.LookPath(binary)
		if err != nil || path == "" {
			return "", fmt.Errorf("找不到 Codex CLI（%s）", binary)
		}
		return path, nil
	}
	if strings.ContainsRune(binary, os.PathSeparator) || strings.Contains(binary, "/") {
		info, err := os.Stat(binary)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("找不到 Codex CLI（%s）", binary)
		}
		return binary, nil
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("找不到 Codex CLI（%s）", binary)
	}
	return path, nil
}

func expandArgs(tmpl []string, vars map[string]string) []string {
	out := make([]string, len(tmpl))
	for i, part := range tmpl {
		for key, value := range vars {
			part = strings.ReplaceAll(part, "{{"+key+"}}", value)
		}
		out[i] = part
	}
	return out
}

func parseCodexStdout(stdout []byte, lastMessagePath string) *RunResult {
	result := &RunResult{}
	for _, line := range bytes.Split(stdout, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
			Item struct {
				ID               string          `json:"id"`
				Type             string          `json:"type"`
				Command          string          `json:"command"`
				AggregatedOutput string          `json:"aggregated_output"`
				Text             string          `json:"text"`
				Status           string          `json:"status"`
				Name             string          `json:"name"`
				Tool             string          `json:"tool"`
				Server           string          `json:"server"`
				Arguments        json.RawMessage `json:"arguments"`
				Result           json.RawMessage `json:"result"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch event.Type {
		case "thread.started":
			if event.ThreadID != "" {
				result.SessionID = event.ThreadID
			}
		case "error":
			if event.Message != "" {
				result.Messages = append(result.Messages, RunnerMessage{Role: RoleSystem, Stage: "错误", Content: event.Message})
			}
		case "turn.failed":
			if event.Error.Message != "" {
				result.Messages = append(result.Messages, RunnerMessage{Role: RoleSystem, Stage: "错误", Content: event.Error.Message})
			}
		case "item.completed":
			appendItem(result, event.Item.ID, event.Item.Type, event.Item.Command, event.Item.AggregatedOutput, event.Item.Text, event.Item.Name, event.Item.Tool, event.Item.Server, event.Item.Arguments, event.Item.Result)
		}
	}
	if fileText, err := os.ReadFile(lastMessagePath); err == nil {
		text := strings.TrimSpace(string(fileText))
		if text != "" && !strings.Contains(result.AssistantText(), text) {
			result.Messages = append(result.Messages, RunnerMessage{Role: RoleAssistant, Stage: "回复", Content: text})
		}
	}
	return result
}

func appendItem(result *RunResult, id, kind, command, output, text, name, tool, server string, args, toolResult json.RawMessage) {
	switch kind {
	case "agent_message":
		if strings.TrimSpace(text) == "" {
			return
		}
		result.Messages = append(result.Messages, RunnerMessage{Role: RoleAssistant, Stage: "回复", Content: text})
	case "command_execution":
		result.Messages = append(result.Messages,
			RunnerMessage{Role: RoleToolCall, Stage: "工具", ToolCallID: id, Content: mustJSON(map[string]any{"tool": "command_execution", "command": command})},
			RunnerMessage{Role: RoleToolResult, Stage: "工具", ToolCallID: id, Content: output},
		)
	case "mcp_tool_call", "web_search":
		toolName := tool
		if toolName == "" {
			toolName = name
		}
		if toolName == "" {
			toolName = kind
		}
		result.Messages = append(result.Messages,
			RunnerMessage{Role: RoleToolCall, Stage: "工具", ToolCallID: id, Content: mustJSON(map[string]any{
				"tool": toolName, "server": server, "arguments": json.RawMessage(emptyObject(args)),
			})},
			RunnerMessage{Role: RoleToolResult, Stage: "工具", ToolCallID: id, Content: string(emptyObject(toolResult))},
		)
	}
}

func emptyObject(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
