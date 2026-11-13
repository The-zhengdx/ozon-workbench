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
	t.Setenv("OZON_CODEX_ARGS_FILE", argsFile)
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
		Prompt:     "follow up",
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
	for _, want := range []string{"exec", "resume", "sess-keep", "--json", "--output-schema", schema, "-o", out, "-C", work, "follow up"} {
		if !strings.Contains(text, want+"\n") && !strings.Contains(text, want) {
			t.Fatalf("args missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(string(result.Output), `"status":"needs_input"`) {
		t.Fatalf("output = %s", result.Output)
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
	if _, err := batCommandLine(`C:\codex.cmd`, []string{"a\nb"}); err == nil {
		t.Fatal("expected newline argument to be rejected")
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
