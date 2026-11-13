package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resolve 把 Codex 给出的相对路径限制在任务目录内。
func Resolve(workDir, filePath string) (string, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", fmt.Errorf("文件路径为空")
	}
	if filepath.IsAbs(filePath) {
		return "", fmt.Errorf("不允许绝对路径")
	}
	clean := filepath.Clean(filePath)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界")
	}
	full := filepath.Join(workDir, clean)
	rel, err := filepath.Rel(workDir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界")
	}
	info, err := os.Lstat(full)
	if err != nil {
		return "", fmt.Errorf("文件不存在")
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return "", fmt.Errorf("不是普通文件")
	}
	if info.Size() <= 0 {
		return "", fmt.Errorf("文件为空")
	}
	return full, nil
}
