package media

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestUnpackRejectsZipSlipAndAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "slip.zip")
	outside := filepath.Join(dir, "outside.txt")
	// A legitimate entry comes first so a partial extract has something to clean up.
	writeOrderedZip(t, zipPath, []zipEntry{
		{"images/ok.txt", "keep-me-not"},
		{"../outside.txt", "nope"},
		{"/tmp/absolute.txt", "nope"},
		{"ok/../../escape.txt", "nope"},
		{`..\windows.txt`, "nope"},
	})
	dest := filepath.Join(dir, "task")
	err := Unpack(zipPath, dest, Limits{MaxBytes: 1 << 20, MaxFiles: 20})
	if err == nil {
		t.Fatal("expected path traversal to fail")
	}
	if !strings.Contains(err.Error(), "不安全路径") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatal("zip should be deleted after a failed extract")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("partial directory should be removed after a failed extract")
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatal("zip slip wrote a file outside the destination")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatal("nested traversal escaped the destination")
	}
}

func TestUnpackEnforcesSizeAndCount(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.zip")
	writeZip(t, big, map[string]string{"a.txt": strings.Repeat("x", 50)})
	dest := filepath.Join(dir, "big-out")
	err := Unpack(big, dest, Limits{MaxBytes: 20, MaxFiles: 10})
	if err == nil || !strings.Contains(err.Error(), "体积超过上限") {
		t.Fatalf("size limit error = %v", err)
	}
	if _, statErr := os.Stat(big); !os.IsNotExist(statErr) {
		t.Fatal("zip should be deleted when the size limit trips")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("partial output should be deleted when the size limit trips")
	}

	many := filepath.Join(dir, "many.zip")
	writeZip(t, many, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c"})
	dest = filepath.Join(dir, "many-out")
	err = Unpack(many, dest, Limits{MaxBytes: 1 << 20, MaxFiles: 2})
	if err == nil || !strings.Contains(err.Error(), "文件数量超过上限") {
		t.Fatalf("count limit error = %v", err)
	}
	if _, statErr := os.Stat(many); !os.IsNotExist(statErr) {
		t.Fatal("zip should be deleted when the file-count limit trips")
	}
}

func TestUnpackGBKNamesAndDeletesZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "cn.zip")
	gbkName, err := simplifiedchinese.GBK.NewEncoder().String("图片/主图.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gbkName, "图片") {
		t.Fatal("encoder returned UTF-8, test would not exercise GBK")
	}
	writeOrderedZip(t, zipPath, []zipEntry{
		{gbkName, "jpeg-bytes"},
		{"readme.txt", "hello"},
	})
	dest := filepath.Join(dir, "task")
	if err := Unpack(zipPath, dest, Limits{MaxBytes: 1 << 20, MaxFiles: 20}); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatal("zip should be deleted after a successful extract")
	}
	got, err := os.ReadFile(filepath.Join(dest, "图片", "主图.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "jpeg-bytes" {
		t.Fatalf("content = %q", got)
	}
	readme, err := os.ReadFile(filepath.Join(dest, "readme.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(readme) != "hello" {
		t.Fatalf("readme = %q", readme)
	}
}

func TestUnpackKeepsUTF8ChineseNames(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "utf8.zip")
	writeZip(t, zipPath, map[string]string{"说明/详情.html": "<html>详情</html>"})
	dest := filepath.Join(dir, "task")
	if err := Unpack(zipPath, dest, Limits{MaxBytes: 1 << 20, MaxFiles: 10}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "说明", "详情.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "详情") {
		t.Fatalf("content = %q", got)
	}
	if _, statErr := os.Stat(zipPath); !os.IsNotExist(statErr) {
		t.Fatal("zip should be deleted after success")
	}
}

type zipEntry struct {
	name string
	body string
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	entries := make([]zipEntry, 0, len(files))
	for name, body := range files {
		entries = append(entries, zipEntry{name, body})
	}
	writeOrderedZip(t, path, entries)
}

func writeOrderedZip(t *testing.T, path string, files []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, file := range files {
		w, err := zw.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
