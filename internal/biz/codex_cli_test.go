package biz

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Re-exec this test binary as a fake `codex`. A shell script is not a valid
// Win32 application, so the fake CLI is the test binary itself.
func TestMain(m *testing.M) {
	flag.Parse()
	if pid := os.Getenv("OZON_CODEX_HELPER_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		if err := runFakeCodex(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err := os.Setenv("OZON_CODEX_HELPER_PID", strconv.Itoa(os.Getpid())); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func runFakeCodex() error {
	argsPath := os.Getenv("OZON_CODEX_ARGS_FILE")
	stdinPath := os.Getenv("OZON_CODEX_STDIN_FILE")
	if argsPath == "" || stdinPath == "" {
		return fmt.Errorf("fake codex: missing OZON_CODEX_ARGS_FILE or OZON_CODEX_STDIN_FILE")
	}
	args := flag.Args()
	var buf strings.Builder
	for _, arg := range args {
		buf.WriteString(arg)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(argsPath, []byte(buf.String()), 0o644); err != nil {
		return err
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	if err := os.WriteFile(stdinPath, stdin, 0o644); err != nil {
		return err
	}
	out := ""
	prev := ""
	for _, arg := range args {
		if prev == "-o" {
			out = arg
		}
		prev = arg
	}
	fmt.Println(`{"type":"thread.started","thread_id":"sess-from-cli"}`)
	fmt.Println(`{"type":"item.completed","item":{"id":"call-1","type":"command_execution","command":"ls","aggregated_output":"index.html","status":"completed"}}`)
	fmt.Println(`{"type":"item.completed","item":{"id":"msg-1","type":"agent_message","text":"hello from codex"}}`)
	if out != "" {
		if err := os.WriteFile(out, []byte("hello from codex\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func testExecutable(t *testing.T) string {
	t.Helper()
	// os.Executable stays valid after the runner sets Cmd.Dir. A relative
	// os.Args[0] would be resolved inside the work directory.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestCLIRunnerParsesJSONLAndResumeArgs(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	t.Setenv("OZON_CODEX_ARGS_FILE", argsFile)
	t.Setenv("OZON_CODEX_STDIN_FILE", stdinFile)
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := NewCLIRunner(CLIConfig{
		Binary: testExecutable(t),
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

func TestResolveBinaryAbsoluteFile(t *testing.T) {
	dir := t.TempDir()
	name := "codex-bin"
	body := []byte("#!/bin/sh\nexit 0\n")
	mode := os.FileMode(0o644)
	if runtime.GOOS == "windows" {
		name = "codex-bin.cmd"
		body = []byte("@echo off\r\n")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("resolveBinary = %s, want %s", got, path)
	}
}

func TestResolveBinaryUsesPath(t *testing.T) {
	dir := t.TempDir()
	name := "ozon-fake-codex"
	fileName := name
	if runtime.GOOS == "windows" {
		fileName += ".cmd"
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte("codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := resolveBinary(name)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("resolveBinary(%s) = %s, want %s", name, got, path)
	}
}

func TestWindowsBatchCommandLine(t *testing.T) {
	line, err := batCommandLine(`C:\Program Files\npm\codex.cmd`, []string{
		"exec", "--json", `C:\Users\A B\%TEMP%`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `cmd.exe /e:ON /v:OFF /d /c ""C:\Program Files\npm\codex.cmd" exec --json "C:\Users\A B\%%cd:~,%TEMP%%cd:~,%""`
	if line != want {
		t.Fatalf("cmdline =\n%s\nwant\n%s", line, want)
	}

	trailing, err := batCommandLine(`C:\codex.cmd`, []string{`C:\foo\`})
	if err != nil {
		t.Fatal(err)
	}
	wantTrailing := `cmd.exe /e:ON /v:OFF /d /c ""C:\codex.cmd" "C:\foo\\""`
	if trailing != wantTrailing {
		t.Fatalf("trailing =\n%s\nwant\n%s", trailing, wantTrailing)
	}

	if _, err := batCommandLine(`C:\a"b.cmd`, nil); err == nil {
		t.Fatal("expected invalid script path")
	}
	if _, err := batCommandLine(`C:\codex.cmd\`, []string{"exec"}); err == nil {
		t.Fatal("expected trailing slash to be rejected")
	}
	if _, err := batCommandLine(`C:\codex.cmd`, []string{"a\nb"}); err == nil {
		t.Fatal("expected newline argument to be rejected")
	}
}

func TestIsWindowsBatch(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\npm\codex.cmd`, true},
		{`C:\npm\codex.CMD`, true},
		{`C:\npm\codex.bat`, true},
		{`C:\npm\codex.BAT`, true},
		{`C:\npm\codex.exe`, false},
		{`codex`, false},
		{`C:\npm\codex.cmd.exe`, false},
	}
	for _, tc := range cases {
		if got := isWindowsBatch(tc.path); got != tc.want {
			t.Errorf("isWindowsBatch(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
