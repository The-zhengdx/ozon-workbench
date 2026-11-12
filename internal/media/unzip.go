// Package media extracts uploaded 1688 zip packages onto the local disk.
package media

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"archive/zip"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Limits cap how much an archive is allowed to expand.
type Limits struct {
	MaxBytes int64
	MaxFiles int
}

// Unpack extracts zipPath into dest, then deletes the zip.
// On any extraction error the partial dest directory and the zip are removed.
func Unpack(zipPath, dest string, limits Limits) error {
	if limits.MaxBytes <= 0 || limits.MaxFiles <= 0 {
		return fmt.Errorf("解压限制未配置")
	}
	err := extract(zipPath, dest, limits)
	if err != nil {
		_ = os.RemoveAll(dest)
		_ = os.Remove(zipPath)
		return err
	}
	if rmErr := os.Remove(zipPath); rmErr != nil && !os.IsNotExist(rmErr) {
		_ = os.RemoveAll(dest)
		return fmt.Errorf("解压成功但删除压缩包失败: %w", rmErr)
	}
	return nil
}

func extract(zipPath, dest string, limits Limits) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("无法打开压缩包: %w", err)
	}
	defer reader.Close()

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}

	var files int
	var total int64
	for _, file := range reader.File {
		name := decodeZipName(file)
		target, err := SafeJoin(destAbs, name)
		if err != nil {
			return fmt.Errorf("压缩包包含不安全路径 %q", displayName(name))
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("压缩包包含符号链接 %q", displayName(name))
		}
		info := file.FileInfo()
		if info.IsDir() || strings.HasSuffix(strings.ReplaceAll(name, "\\", "/"), "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}
		files++
		if files > limits.MaxFiles {
			return fmt.Errorf("压缩包文件数量超过上限 %d", limits.MaxFiles)
		}
		declared := int64(file.UncompressedSize64)
		if declared < 0 || declared > limits.MaxBytes || total > limits.MaxBytes-declared {
			return fmt.Errorf("解压后体积超过上限 %d 字节", limits.MaxBytes)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		written, err := writeLimited(file, target, limits.MaxBytes-total)
		total += written
		if err != nil {
			return err
		}
		if total > limits.MaxBytes {
			return fmt.Errorf("解压后体积超过上限 %d 字节", limits.MaxBytes)
		}
	}
	if files == 0 {
		return fmt.Errorf("压缩包里没有文件")
	}
	return nil
}

func writeLimited(file *zip.File, target string, budget int64) (int64, error) {
	src, err := file.Open()
	if err != nil {
		return 0, fmt.Errorf("读取压缩包条目失败: %w", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("写入解压文件失败: %w", err)
	}
	defer dst.Close()
	written, err := io.Copy(dst, io.LimitReader(src, budget+1))
	if err != nil {
		return written, fmt.Errorf("写入解压文件失败: %w", err)
	}
	if written > budget {
		return written, fmt.Errorf("解压后体积超过上限")
	}
	// Drain one extra byte to catch entries that ignored the declared size.
	var probe [1]byte
	n, readErr := src.Read(probe[:])
	if n > 0 {
		return written + int64(n), fmt.Errorf("解压后体积超过上限")
	}
	if readErr != nil && readErr != io.EOF {
		return written, fmt.Errorf("读取压缩包条目失败: %w", readErr)
	}
	return written, nil
}

// SafeJoin resolves a zip entry name under root and rejects absolute paths and traversal.
func SafeJoin(root, name string) (string, error) {
	rel, err := safeRel(name)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootAbs, filepath.FromSlash(rel))
	if err := within(rootAbs, target); err != nil {
		return "", err
	}
	return target, nil
}

func safeRel(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("空路径")
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("路径含空字节")
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "//") {
		return "", fmt.Errorf("绝对路径")
	}
	if vol := filepath.VolumeName(name); vol != "" {
		return "", fmt.Errorf("绝对路径")
	}
	// Windows drive letters survive the slash rewrite as "C:/...".
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("绝对路径")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("路径穿越")
	}
	return clean, nil
}

func within(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("路径穿越")
	}
	return nil
}

// decodeZipName returns a UTF-8 name. Windows Chinese zips often store GBK
// without the UTF-8 flag (bit 11). Those entries are reported as NonUTF8
// and the name bytes are not valid UTF-8.
func decodeZipName(file *zip.File) string {
	name := file.Name
	if file.Flags&0x800 != 0 && utf8.ValidString(name) {
		return name
	}
	if file.NonUTF8 || !utf8.ValidString(name) {
		decoded, err := simplifiedchinese.GBK.NewDecoder().String(name)
		if err == nil && decoded != "" && utf8.ValidString(decoded) && !strings.ContainsRune(decoded, '\uFFFD') {
			return decoded
		}
	}
	return name
}

func displayName(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	return fmt.Sprintf("%q", name)
}
