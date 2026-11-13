package codexcli

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

func TestMain(m *testing.M) {
	if pid := os.Getenv("OZON_CODEX_HELPER_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		if err := runFakeCodex(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	flag.Parse()
	if err := os.Setenv("OZON_CODEX_HELPER_PID", strconv.Itoa(os.Getpid())); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func runFakeCodex() error {
	argsPath := os.Getenv("OZON_CODEX_ARGS_FILE")
	if argsPath == "" {
		return fmt.Errorf("fake codex: missing OZON_CODEX_ARGS_FILE")
	}
	args := os.Args[1:]
	var buf strings.Builder
	for _, arg := range args {
		buf.WriteString(arg)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(argsPath, []byte(buf.String()), 0o644); err != nil {
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
	if stdinPath := os.Getenv("OZON_CODEX_STDIN_FILE"); stdinPath != "" {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		if err := os.WriteFile(stdinPath, body, 0o644); err != nil {
			return err
		}
	}
	fmt.Println(`{"type":"thread.started","thread_id":"sess-from-cli"}`)
	if out != "" {
		body := []byte(`{"schema_version":"1.2","status":"needs_input","stage":"extract","message":"need","questions":[{"field":"cost.domestic_shipping","text":"fee","required":true}],"review":null,"data":{"products":[],"visual_plan":[],"media":[],"warnings":[]},"error":null}`)
		if err := os.WriteFile(out, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func testExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestCLIRunnerWritesSchemaArgsAndSession(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	t.Setenv("OZON_CODEX_ARGS_FILE", argsFile)
	t.Setenv("OZON_CODEX_STDIN_FILE", stdinFile)
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schema, SchemaJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(work, "turn.json")
	result, err := CLI{}.Exec(context.Background(), ExecRequest{
		Binary:     testExecutable(t),
		WorkDir:    work,
		SessionID:  "sess-keep",
		Prompt:     "中文提示\n第二行",
		SchemaPath: schema,
		OutputPath: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != "sess-from-cli" {
		t.Fatalf("session = %s", result.SessionID)
	}
	body, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"exec", "resume", "sess-keep", "--json", "--output-schema", schema, "-o", out, "-"} {
		if !strings.Contains(text, want+"\n") {
			t.Fatalf("args missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--sandbox\n") || strings.Contains(text, "-C\n") || strings.Contains(text, "中文提示") {
		t.Fatalf("resume argv must not include sandbox, -C, or the prompt:\n%s", text)
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "中文提示\n第二行" {
		t.Fatalf("stdin = %q", stdin)
	}
	if !strings.Contains(string(result.Output), `"status":"needs_input"`) {
		t.Fatalf("output = %s", result.Output)
	}
}

func TestRelativeWorkDirIsAbsoluteInArgv(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workAbs := filepath.Join(dir, "work")
	if err := os.MkdirAll(workAbs, 0o755); err != nil {
		t.Fatal(err)
	}
	relWork, err := filepath.Rel(cwd, workAbs)
	if err != nil || filepath.IsAbs(relWork) {
		t.Fatalf("relative work = %s (%v)", relWork, err)
	}
	schemaAbs := filepath.Join(workAbs, "schema.json")
	if err := os.WriteFile(schemaAbs, SchemaJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	relSchema, err := filepath.Rel(cwd, schemaAbs)
	if err != nil {
		t.Fatal(err)
	}
	outAbs := filepath.Join(workAbs, "turn.json")
	relOut, err := filepath.Rel(cwd, outAbs)
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "args.txt")
	t.Setenv("OZON_CODEX_ARGS_FILE", argsFile)
	_, err = CLI{}.Exec(context.Background(), ExecRequest{
		Binary: testExecutable(t), WorkDir: relWork, Prompt: "ping",
		SchemaPath: relSchema, OutputPath: relOut,
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--sandbox", "workspace-write", "-C", workAbs, schemaAbs, outAbs, "-"} {
		if !strings.Contains(string(text), want+"\n") {
			t.Fatalf("args missing %q:\n%s", want, text)
		}
	}
}

func TestResolveBinaryAbsoluteAndPath(t *testing.T) {
	dir := t.TempDir()
	name := "codex-bin"
	if runtime.GOOS == "windows" {
		name = "codex-bin.cmd"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path && !strings.EqualFold(got, path) {
		t.Fatalf("resolveBinary = %s, want %s", got, path)
	}

	binName := "ozon-fake-codex"
	fileName := binName
	if runtime.GOOS == "windows" {
		fileName += ".cmd"
	}
	path = filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte("codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err = resolveBinary(binName)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != fileName && !strings.EqualFold(filepath.Base(got), fileName) {
		t.Fatalf("LookPath = %s", got)
	}
}

func TestWindowsBatchCommandLine(t *testing.T) {
	line, err := batCommandLine(`C:\Program Files\npm\codex.cmd`, []string{"exec", "--json", `C:\Users\A B\%TEMP%`})
	if err != nil {
		t.Fatal(err)
	}
	want := `cmd.exe /e:ON /v:OFF /d /c ""C:\Program Files\npm\codex.cmd" exec --json "C:\Users\A B\%%cd:~,%TEMP%%cd:~,%""`
	if line != want {
		t.Fatalf("cmdline =\n%s\nwant\n%s", line, want)
	}
	if _, err := batCommandLine(`C:\codex.cmd`, []string{"a\nb"}); err == nil || !strings.Contains(err.Error(), "Codex 参数无效") {
		t.Fatal(err)
	}
}

func TestExecArgvOnEitherOS(t *testing.T) {
	work := `C:\tasks\帽子 1`
	schema := `C:\tasks\帽子 1\schema.json`
	out := `C:\tasks\帽子 1\turn.json`
	first := BuildArgs(ExecRequest{WorkDir: work, SchemaPath: schema, OutputPath: out, Prompt: "中文\n第二行"})
	wantFirst := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "workspace-write", "--output-schema", schema, "-o", out, "-C", work, "-"}
	if strings.Join(first, "\n") != strings.Join(wantFirst, "\n") {
		t.Fatalf("exec argv =\n%s", strings.Join(first, "\n"))
	}
	resumed := BuildArgs(ExecRequest{WorkDir: work, SessionID: "sess-1", SchemaPath: schema, OutputPath: out, Prompt: "继续"})
	wantResume := []string{"exec", "resume", "--json", "--skip-git-repo-check", "--output-schema", schema, "-o", out, "sess-1", "-"}
	if strings.Join(resumed, "\n") != strings.Join(wantResume, "\n") {
		t.Fatalf("resume argv =\n%s", strings.Join(resumed, "\n"))
	}
	for _, arg := range append(first, resumed...) {
		if strings.Contains(arg, "\n") || strings.Contains(arg, "中文") && arg != work && arg != schema && arg != out {
			t.Fatalf("prompt leaked into argv %q", arg)
		}
	}
}

func TestWindowsCmdResolvesToNodeScript(t *testing.T) {
	root := t.TempDir()
	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "node"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", nodeDir)
	script := filepath.Join(root, "node_modules", "@openai", "codex", "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("console.log('codex')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdPath := filepath.Join(root, "codex.cmd")
	if err := os.WriteFile(cmdPath, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe, lead, err := resolveLaunch(cmdPath)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "node" || len(lead) != 1 || lead[0] != script {
		t.Fatalf("launch = %s %v", exe, lead)
	}
}

func TestSchemaIsStrictStructuredOutput(t *testing.T) {
	problems := StrictProblems(SchemaJSON)
	if len(problems) > 0 {
		t.Fatalf("schema:\n%s", strings.Join(problems, "\n"))
	}
}

func TestIsWindowsBatch(t *testing.T) {
	if !isWindowsBatch(`C:\npm\codex.cmd`) || !isWindowsBatch(`C:\npm\CODEX.BAT`) {
		t.Fatal("batch extensions")
	}
	if isWindowsBatch(`C:\npm\codex.exe`) || isWindowsBatch(`codex`) {
		t.Fatal("not batch")
	}
}

func TestExamplesMatchSchema(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "requirements-v2", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || strings.Contains(entry.Name(), "queued") || strings.Contains(entry.Name(), "system_") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateSchema(SchemaJSON, body); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		reply, err := ParseReply(body)
		if err != nil {
			t.Fatal(err)
		}
		if err := BusinessError(reply); err != nil {
			t.Fatalf("%s business: %v", entry.Name(), err)
		}
	}
	if _, err := io.ReadAll(strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
}
