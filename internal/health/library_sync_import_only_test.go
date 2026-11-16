package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/metadata"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With cleanup on, a file linked only from the import dir is dropped by the
// delete pass. The add pass must not register it again, or every sync deletes
// and re-adds the same records (and re-reads their metadata) forever.
func TestSyncLibrary_ImportOnlyFileIsNotReAdded(t *testing.T) {
	repo, db := setupWorkerTestDB(t)
	_, err := db.Exec(`CREATE TABLE system_state (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	root := t.TempDir()
	mountDir := filepath.Join(root, "mount")
	libraryDir := filepath.Join(root, "library")
	importDir := filepath.Join(root, "import")
	metaDir := filepath.Join(root, "metadata")
	for _, dir := range []string{mountDir, libraryDir, importDir, metaDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	ms := metadata.NewMetadataService(metaDir)
	write := func(virtualPath string) string {
		t.Helper()
		target := filepath.Join(mountDir, filepath.FromSlash(virtualPath))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
		meta := ms.CreateFileMetadata(1, "r.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
			nil, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "")
		require.NoError(t, ms.WriteFileMetadata(virtualPath, meta))
		return target
	}
	inLibrary := "mm/a/in-library.mkv"
	importOnly := "mm/b/import-only.mkv"
	require.NoError(t, os.Symlink(write(inLibrary), filepath.Join(libraryDir, "in-library.mkv")))
	require.NoError(t, os.Symlink(write(importOnly), filepath.Join(importDir, "import-only.mkv")))

	cfg := config.DefaultConfig()
	cleanup := true
	cfg.Health.CleanupOrphanedMetadata = &cleanup
	cfg.Import.ImportStrategy = config.ImportStrategySYMLINK
	cfg.Import.ImportDir = &importDir
	cfg.Health.LibraryDir = &libraryDir
	cfg.Metadata.RootPath = metaDir
	cfg.MountPath = mountDir
	manager := config.NewManager(cfg, "")
	worker := NewLibrarySyncWorker(ms, repo, manager.GetConfig, manager, &MockRcloneClient{})
	ctx := context.Background()

	// A record created at import time for the import-only file is removed once.
	_, err = db.Exec(`INSERT INTO file_health (file_path, status) VALUES (?, 'healthy')`, importOnly)
	require.NoError(t, err)

	for pass := range 3 {
		worker.SyncLibrary(ctx, false)
		h, err := repo.GetFileHealth(ctx, inLibrary)
		require.NoError(t, err)
		assert.NotNil(t, h, "library file stays tracked, pass %d", pass)
		h, err = repo.GetFileHealth(ctx, importOnly)
		require.NoError(t, err)
		assert.Nil(t, h, "import-only file is not tracked, pass %d", pass)
		require.NotNil(t, worker.lastSyncResult)
		wantAdded := 0
		if pass == 0 {
			wantAdded = 1
		}
		assert.Equal(t, wantAdded, worker.lastSyncResult.FilesAdded,
			"only the library file is ever added, pass %d", pass)
		assert.True(t, ms.FileExists(importOnly), "metadata itself is untouched")
	}
}
