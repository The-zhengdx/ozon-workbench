//go:build !windows

package codexcli

import (
	"context"
	"os/exec"
)

func commandContext(ctx context.Context, binary string, args []string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, binary, args...), nil
}
