package codexcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveLaunch turns a Windows npm shim (codex.cmd) into `node .../codex.js`
// so arguments never pass through cmd.exe. Other platforms keep the resolved binary.
func resolveLaunch(binary string) (string, []string, error) {
	path, err := resolveBinary(binary)
	if err != nil {
		return "", nil, err
	}
	if !isWindowsBatch(path) {
		return path, nil, nil
	}
	script := findCodexJS(path)
	if script == "" {
		return path, nil, nil
	}
	node, err := exec.LookPath("node")
	if err != nil || node == "" {
		return "", nil, fmt.Errorf("找到 %s，但 PATH 里没有 node，不能避开 cmd.exe 启动 Codex", script)
	}
	return node, []string{script}, nil
}

func findCodexJS(cmdPath string) string {
	dir := filepath.Dir(cmdPath)
	candidates := []string{
		filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.js"),
		filepath.Join(dir, "..", "node_modules", "@openai", "codex", "bin", "codex.js"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	body, err := os.ReadFile(cmdPath)
	if err != nil {
		return ""
	}
	text := string(body)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(strings.ToLower(line), "codex.js") {
			continue
		}
		for _, field := range strings.Fields(strings.ReplaceAll(line, `"`, " ")) {
			if strings.HasSuffix(strings.ToLower(field), "codex.js") && !strings.Contains(field, "%") {
				if info, err := os.Stat(field); err == nil && !info.IsDir() {
					return field
				}
			}
		}
	}
	return ""
}
