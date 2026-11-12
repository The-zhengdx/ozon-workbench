package biz

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRunnerParsesJSONLAndResumeArgs(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex.sh")
	argsFile := filepath.Join(dir, "args.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"" + argsFile + "\"\n" +
		"cat > \"" + stdinFile + "\"\n" +
		"out=\nprev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"-o\" ]; then out=\"$a\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"sess-from-cli\"}'\n" +
		"printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"id\":\"call-1\",\"type\":\"command_execution\",\"command\":\"ls\",\"aggregated_output\":\"index.html\",\"status\":\"completed\"}}'\n" +
		"printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"id\":\"msg-1\",\"type\":\"agent_message\",\"text\":\"hello from codex\"}}'\n" +
		"if [ -n \"$out\" ]; then printf '%s\\n' 'hello from codex' > \"$out\"; fi\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := NewCLIRunner(CLIConfig{
		Binary: script,
		NewArgs: []string{
			"exec", "--json", "--skip-git-repo-check", "--sandbox", "workspace-write",
			"-C", "{{work_dir}}", "-o", "{{last_message}}", "-",
		},
		ResumeArgs: []string{
			"exec", "resume", "{{session_id}}", "--json", "--skip-git-repo-check",
			"-C", "{{work_dir}}", "-o", "{{last_message}}", "-",
		},
	})
	result, err := runner.Run(context.Background(), RunRequest{
		Stage: StageExtract, Prompt: "prompt body", WorkDir: work, MasterSKU: "30",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "sess-from-cli" {
		t.Fatalf("session = %s", result.SessionID)
	}
	if result.AssistantText() != "hello from codex" {
		t.Fatalf("assistant = %q", result.AssistantText())
	}
	var sawCall, sawResult bool
	for _, msg := range result.Messages {
		if msg.Role == RoleToolCall && msg.ToolCallID == "call-1" && strings.Contains(msg.Content, "ls") {
			sawCall = true
		}
		if msg.Role == RoleToolResult && msg.ToolCallID == "call-1" && strings.Contains(msg.Content, "index.html") {
			sawResult = true
		}
	}
	if !sawCall || !sawResult {
		t.Fatalf("tool messages = %#v", result.Messages)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argText := string(args)
	for _, want := range []string{"exec", "--json", "--skip-git-repo-check", "workspace-write", work} {
		if !strings.Contains(argText, want) {
			t.Fatalf("args missing %s: %s", want, argText)
		}
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "prompt body" {
		t.Fatalf("stdin = %q", stdin)
	}

	resumed, err := runner.Run(context.Background(), RunRequest{
		Stage: StageFollowUp, SessionID: "sess-from-cli", Prompt: "follow up", WorkDir: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.SessionID != "sess-from-cli" {
		t.Fatalf("resumed session = %s", resumed.SessionID)
	}
	args, err = os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argText = string(args)
	if !strings.Contains(argText, "resume") || !strings.Contains(argText, "sess-from-cli") {
		t.Fatalf("resume args = %s", argText)
	}
}

func TestCLIRunnerMissingBinary(t *testing.T) {
	runner := NewCLIRunner(CLIConfig{Binary: "codex-not-installed", NewArgs: []string{"exec", "-"}})
	_, err := runner.Run(context.Background(), RunRequest{Stage: StageExtract, WorkDir: t.TempDir(), Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "找不到 Codex CLI") {
		t.Fatalf("err = %v", err)
	}
}
