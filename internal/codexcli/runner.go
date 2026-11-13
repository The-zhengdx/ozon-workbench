package codexcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ExecRequest 是一轮非交互 Codex。SessionID 为空时使用 exec，否则使用 exec resume。
type ExecRequest struct {
	Binary     string
	WorkDir    string
	SessionID  string
	Prompt     string
	SchemaPath string
	OutputPath string
	ExtraArgs  []string
}

type ExecResult struct {
	SessionID string
	ExitCode  int
	Output    []byte
	Stdout    string
	Stderr    string
}

type Runner interface {
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
}

// CLI 通过 argv 调用本机 Codex，不拼接 shell 字符串。
type CLI struct{}

func BuildArgs(req ExecRequest) []string {
	args := []string{"exec"}
	if strings.TrimSpace(req.SessionID) != "" {
		args = append(args, "resume", req.SessionID)
	}
	args = append(args, "--json", "--skip-git-repo-check", "--sandbox", "workspace-write")
	args = append(args, req.ExtraArgs...)
	args = append(args, "--output-schema", req.SchemaPath, "-o", req.OutputPath, "-C", req.WorkDir, req.Prompt)
	return args
}

func (CLI) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	binary := strings.TrimSpace(req.Binary)
	if binary == "" {
		binary = "codex"
	}
	if strings.TrimSpace(req.WorkDir) == "" || strings.TrimSpace(req.SchemaPath) == "" || strings.TrimSpace(req.OutputPath) == "" {
		return ExecResult{}, fmt.Errorf("Codex 工作目录、schema 或输出文件为空")
	}
	bin, err := resolveBinary(binary)
	if err != nil {
		return ExecResult{}, err
	}
	args := BuildArgs(req)
	cmd, err := commandContext(ctx, bin, args)
	if err != nil {
		return ExecResult{}, err
	}
	cmd.Dir = req.WorkDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	result := ExecResult{Stdout: stdout.String(), Stderr: redact(stderr.String()), SessionID: sessionFromJSONL(stdout.Bytes())}
	if result.SessionID == "" {
		result.SessionID = req.SessionID
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if body, err := os.ReadFile(req.OutputPath); err == nil {
		result.Output = body
	}
	if runErr != nil {
		msg := strings.TrimSpace(result.Stderr)
		if msg == "" {
			msg = runErr.Error()
		}
		return result, fmt.Errorf("Codex CLI 执行失败：%s", truncate(msg, 2000))
	}
	return result, nil
}

func sessionFromJSONL(stdout []byte) string {
	for _, line := range bytes.Split(stdout, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Type == "thread.started" && event.ThreadID != "" {
			return event.ThreadID
		}
	}
	return ""
}

func resolveBinary(binary string) (string, error) {
	if runtime.GOOS == "windows" {
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
	if err != nil || path == "" {
		return "", fmt.Errorf("找不到 Codex CLI（%s）", binary)
	}
	return path, nil
}

func redact(s string) string {
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// CopyStdout 便于测试读取子进程输出。
func CopyStdout(w io.Writer, r io.Reader) error {
	_, err := io.Copy(w, r)
	return err
}

func SchemaPath(dir string) (string, error) {
	path := filepath.Join(dir, "02_codex_reply_v1_2.schema.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, SchemaJSON, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
