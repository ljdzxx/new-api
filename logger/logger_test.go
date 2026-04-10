package logger

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestCleanupOldLogFiles_KeepMaxAndDeleteOldest(t *testing.T) {
	dir := t.TempDir()

	logFiles := []string{
		"oneapi-20260401.log",
		"oneapi-20260402.log",
		"oneapi-20260403.log",
		"oneapi-20260404.log",
		"oneapi-20260405.log",
		"oneapi-20260406.log",
		"oneapi-20260407.log",
		"oneapi-20260408.log",
		"oneapi-20260409.log",
	}
	for _, name := range logFiles {
		mustWriteFile(t, filepath.Join(dir, name))
	}
	// Non-log files should not be touched.
	mustWriteFile(t, filepath.Join(dir, "keep-me.txt"))

	cleanupOldLogFiles(dir, 7)

	got := listFilesByPrefixSuffix(t, dir, logFilePrefix, logFileSuffix)
	want := []string{
		"oneapi-20260403.log",
		"oneapi-20260404.log",
		"oneapi-20260405.log",
		"oneapi-20260406.log",
		"oneapi-20260407.log",
		"oneapi-20260408.log",
		"oneapi-20260409.log",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected retained log files, got=%v want=%v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep-me.txt")); err != nil {
		t.Fatalf("non-log file should be kept, stat err=%v", err)
	}
}

func TestCleanupOldLogFiles_PreferFilenameTimeOverModTime(t *testing.T) {
	dir := t.TempDir()

	f1 := filepath.Join(dir, "oneapi-20260408120000.log")
	f2 := filepath.Join(dir, "oneapi-20260409120000.log")
	f3 := filepath.Join(dir, "oneapi-20260410120000.log")
	mustWriteFile(t, f1)
	mustWriteFile(t, f2)
	mustWriteFile(t, f3)

	// Intentionally make modtime opposite to file name time to verify name-time sorting priority.
	now := time.Now()
	mustSetFileTime(t, f1, now.Add(2*time.Hour))
	mustSetFileTime(t, f2, now.Add(1*time.Hour))
	mustSetFileTime(t, f3, now.Add(-1*time.Hour))

	cleanupOldLogFiles(dir, 2)

	got := listFilesByPrefixSuffix(t, dir, logFilePrefix, logFileSuffix)
	want := []string{
		"oneapi-20260409120000.log",
		"oneapi-20260410120000.log",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected retained log files, got=%v want=%v", got, want)
	}
}

func mustWriteFile(t *testing.T, filePath string) {
	t.Helper()
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("write file %s failed: %v", filePath, err)
	}
}

func mustSetFileTime(t *testing.T, filePath string, ts time.Time) {
	t.Helper()
	if err := os.Chtimes(filePath, ts, ts); err != nil {
		t.Fatalf("set file time %s failed: %v", filePath, err)
	}
}

func listFilesByPrefixSuffix(t *testing.T, dir, prefix, suffix string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir failed: %v", err)
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) >= len(prefix)+len(suffix) && name[:len(prefix)] == prefix && name[len(name)-len(suffix):] == suffix {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
