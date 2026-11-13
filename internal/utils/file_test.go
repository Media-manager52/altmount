package utils

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMoveFile_Normal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "altmount-move-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	content := []byte("hello altmount")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	if err := MoveFile(src, dst); err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	// Verify dst exists and src is gone
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("expected source file to be gone, but stat got: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("got content %q, want %q", got, content)
	}
}

func TestMoveFileMissingSourceAfterCopyIsSuccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "altmount-move-missing-source")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	src := filepath.Join(tmpDir, "missing.nzb")
	dst := filepath.Join(tmpDir, "persistent.nzb")
	if err := os.WriteFile(dst, []byte("complete persistent copy"), 0644); err != nil {
		t.Fatal(err)
	}
	// This is the race that produced "failed to remove source file after
	// successful copy": the copy is complete, but another worker won the
	// source deletion. It must not fail the import.
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := removeSourceAfterCopy(src); err != nil {
		t.Fatalf("absent source was not treated as success: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "complete persistent copy" {
		t.Fatalf("persistent copy=%q err=%v", got, err)
	}
}

func TestIsCrossDeviceError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("some standard error"), false},
		{errors.New("invalid cross-device link"), true},
		{errors.New("cross-device link error"), true},
		{errors.New("EXDEV: invalid cross-device link"), true},
		{&os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EXDEV}, true},
		{&os.PathError{Op: "rename", Path: "a", Err: syscall.EXDEV}, true},
		// Windows reports ERROR_NOT_SAME_DEVICE, whose message never mentions
		// "cross-device". Match it so the copy-and-delete fallback kicks in there too.
		{errors.New("The system cannot move the file to a different disk drive."), true},
		{&os.LinkError{
			Op:  "rename",
			Old: "a",
			New: "b",
			Err: errors.New("The system cannot move the file to a different disk drive."),
		}, true},
	}

	for _, tt := range tests {
		if got := isCrossDeviceError(tt.err); got != tt.want {
			t.Errorf("isCrossDeviceError(%v) = %v; want %v", tt.err, got, tt.want)
		}
	}
}
