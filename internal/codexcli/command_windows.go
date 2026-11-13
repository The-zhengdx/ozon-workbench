//go:build windows

package codexcli

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func commandContext(ctx context.Context, binary string, args []string) (*exec.Cmd, error) {
	if !isWindowsBatch(binary) {
		return exec.CommandContext(ctx, binary, args...), nil
	}
	cmdline, err := batCommandLine(binary, args)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, comSpec())
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline}
	return cmd, nil
}

func comSpec() string {
	if p := strings.TrimSpace(os.Getenv("ComSpec")); p != "" {
		return p
	}
	if p, err := exec.LookPath("cmd"); err == nil && p != "" {
		return p
	}
	return "cmd.exe"
}
