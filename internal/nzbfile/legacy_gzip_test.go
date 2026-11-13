package nzbfile

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenLegacyGzipRenamedToPlainNZB(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "original.nzb")
	stored := filepath.Join(dir, "failed.nzb")
	content := []byte(`<?xml version="1.0"?><nzb><file subject="movie.mkv"/></nzb>`)
	if err := os.WriteFile(plain, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Compress(plain, stored); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(stored)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actual, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(content) {
		t.Fatalf("legacy failed NZB returned compressed data instead of XML")
	}
}
