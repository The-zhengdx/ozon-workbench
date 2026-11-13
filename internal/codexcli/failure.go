package codexcli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var bearerSecret = regexp.MustCompile(`(?i)bearer\s+\S+`)

// ConfigFailure reports an invocation problem: the process never started,
// clap rejected the flags, or the output schema was refused.
// Those are not transient model failures and should not be recorded as a cancel.
func ConfigFailure(result ExecResult, runErr error) bool {
	if runErr != nil && !result.Started {
		return true
	}
	if result.ExitCode == 2 {
		return true
	}
	blob := result.Stderr + "\n" + result.Stdout + "\n"
	if runErr != nil {
		blob += runErr.Error()
	}
	for _, needle := range []string{
		"invalid_json_schema",
		"unexpected argument",
		"Failed to read output schema",
		"error: unexpected",
		"Codex 参数无效",
		"Codex CLI 路径无效",
	} {
		if strings.Contains(blob, needle) {
			return true
		}
	}
	return false
}

// FormatFailure is the task event text: argv, exit code, and the stderr tail.
func FormatFailure(result ExecResult, runErr error) string {
	var b strings.Builder
	if runErr != nil {
		b.WriteString(strings.TrimSpace(runErr.Error()))
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "exit=%d\nargv:", result.ExitCode)
	for _, arg := range result.Args {
		b.WriteByte(' ')
		if arg == "" || strings.ContainsAny(arg, " \t\"") {
			b.WriteString(strconv.Quote(arg))
		} else {
			b.WriteString(arg)
		}
	}
	if tail := tailText(result.Stderr, 1200); tail != "" {
		b.WriteString("\nstderr:\n")
		b.WriteString(tail)
	}
	if tail := tailText(stdoutErrors(result.Stdout), 800); tail != "" {
		b.WriteString("\nstdout:\n")
		b.WriteString(tail)
	}
	return bearerSecret.ReplaceAllString(b.String(), "Bearer [redacted]")
}

func stdoutErrors(stdout string) string {
	var b strings.Builder
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `"type":"error"`) || strings.Contains(line, `"type": "error"`) || strings.Contains(line, "invalid_json_schema") {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
