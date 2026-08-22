package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPairingTokenRotationScopeAndRevocation(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	code, _, err := db.CreatePairingCode(ctx, "vault-a", time.Minute, "sync:read,sync:write")
	if err != nil {
		t.Fatal(err)
	}
	paired, err := db.PairDevice(ctx, "vault-a", code, "Laptop", "windows", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if paired.Token == "" || paired.Device.ID == "" {
		t.Fatalf("paired=%+v", paired)
	}
	if _, err := db.PairDevice(ctx, "vault-a", code, "Again", "windows", "1.0.0"); err == nil {
		t.Fatal("pairing code was reused")
	}
	principal, err := db.AuthenticateToken(ctx, paired.Token, "vault-a", "sync:write")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateToken(ctx, paired.Token, "vault-a", "history:read"); err == nil {
		t.Fatal("scope escalation succeeded")
	}
	rotated, err := db.RotateToken(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateToken(ctx, paired.Token, "vault-a", "sync:read"); err == nil {
		t.Fatal("old token survived rotation")
	}
	rotatedPrincipal, err := db.AuthenticateToken(ctx, rotated, "vault-a", "sync:read")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceStatus(ctx, "vault-a", rotatedPrincipal.DeviceID, "revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateToken(ctx, rotated, "vault-a", "sync:read"); err == nil {
		t.Fatal("revoked token authenticated")
	}
}

func TestAcknowledgementAndGCSafetyPlan(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	code, _, _ := db.CreatePairingCode(ctx, "vault-a", time.Minute, DefaultDeviceScopes)
	paired, err := db.PairDevice(ctx, "vault-a", code, "Device", "test", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("old content")
	created, _, err := db.Put(ctx, "vault-a", "old.md", paired.Device.ID, 0, 1, Hash(content), content)
	if err != nil {
		t.Fatal(err)
	}
	deleted, _, err := db.Delete(ctx, "vault-a", "old.md", paired.Device.ID, created.Revision, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE changes SET created_at=? WHERE vault_id='vault-a'`, time.Now().Add(-48*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}

	blocked, err := db.BuildGCPlan(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.ChangeRevisions) != 0 {
		t.Fatalf("GC ignored unacknowledged device: %+v", blocked)
	}
	if err := db.AcknowledgeDevice(ctx, "vault-a", paired.Device.ID, deleted.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := db.BuildGCPlan(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangeRevisions) != 2 || len(plan.DeletedPaths) != 1 || plan.Hash == "" {
		t.Fatalf("plan=%+v", plan)
	}
	if _, err := db.ExecuteGCPlan(ctx, plan.ID, "wrong"); err == nil {
		t.Fatal("GC accepted wrong plan hash")
	}
	result, err := db.ExecuteGCPlan(ctx, plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangesDeleted != 2 || result.PathsDeleted != 1 {
		t.Fatalf("result=%+v", result)
	}
	snapshot, _, err := db.ListSnapshot(ctx, "vault-a", 1<<62, "", 10)
	if err != nil || len(snapshot) != 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestResourceLimits(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	db.ConfigureLimits(ResourceLimits{MaxFileBytes: 4, DefaultQuotaBytes: 5, DefaultMaxFiles: 1})
	content := []byte("1234")
	if _, _, err := db.Put(ctx, "vault-a", "one.bin", "device", 0, 1, Hash(content), content); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Put(ctx, "vault-a", "two.bin", "device", 0, 1, Hash([]byte("x")), []byte("x")); !isResourceError(err, "vault_file_limit_exceeded") {
		t.Fatalf("file limit err=%v", err)
	}
	if _, _, err := db.Put(ctx, "vault-a", "one.bin", "device", 1, 2, Hash([]byte("12345")), []byte("12345")); !isResourceError(err, "file_too_large") {
		t.Fatalf("max file err=%v", err)
	}
	db.ConfigureLimits(ResourceLimits{MinFreeBytes: int64(^uint64(0) >> 1)})
	if _, err := db.PutChunk(ctx, Hash([]byte("x")), []byte("x")); !isResourceError(err, "insufficient_storage") {
		t.Fatalf("disk reserve err=%v", err)
	}
}

func TestBackupVerifyRestoreAndDoctor(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	content := []byte("backup content")
	change, _, err := db.Put(ctx, "vault-a", "note.md", "device", 0, 1, Hash(content), content)
	if err != nil {
		t.Fatal(err)
	}
	chunk := []byte("chunk")
	if _, err := db.PutChunk(ctx, Hash(chunk), chunk); err != nil {
		t.Fatal(err)
	}

	backup := t.TempDir()
	manifest, err := db.Backup(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files["sync.db"] == "" {
		t.Fatalf("manifest=%+v", manifest)
	}
	if _, err := VerifyBackup(backup); err != nil {
		t.Fatal(err)
	}
	restore := t.TempDir()
	if err := RestoreBackup(backup, restore); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(filepath.Join(restore, "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	data, err := restored.GetBlob(ctx, "vault-a", change.BlobHash)
	if err != nil || string(data) != string(content) {
		t.Fatalf("restored data=%q err=%v", data, err)
	}
	report, err := restored.Doctor(ctx)
	if err != nil || !report.OK {
		t.Fatalf("doctor=%+v err=%v", report, err)
	}

	if err := os.WriteFile(filepath.Join(backup, "sync.db"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(backup); err == nil {
		t.Fatal("tampered backup verified")
	}
}

func TestDoctorDetectsMissingAndCorruptChunks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestStore(t)
	first := []byte("first chunk")
	second := []byte("second chunk")
	firstHash := Hash(first)
	secondHash := Hash(second)
	if _, err := db.PutChunk(ctx, firstHash, first); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PutChunk(ctx, secondHash, second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(db.blobDir, filepath.FromSlash(chunkRelativePath(firstHash)))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(db.blobDir, filepath.FromSlash(chunkRelativePath(secondHash))), []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := db.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || len(report.MissingChunkHashes) != 1 || report.MissingChunkHashes[0] != firstHash || len(report.CorruptChunkHashes) != 1 || report.CorruptChunkHashes[0] != secondHash {
		t.Fatalf("doctor report=%+v", report)
	}
}

func TestDoctorRejectsChunkPathsOutsideBlobDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestStore(t)
	data := []byte("do not read outside the blob directory")
	hash := Hash(data)
	if _, err := db.PutChunk(ctx, hash, data); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE chunks SET relative_path=? WHERE hash=?`, "../../outside", hash); err != nil {
		t.Fatal(err)
	}

	report, err := db.Doctor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || len(report.CorruptChunkHashes) != 1 || report.CorruptChunkHashes[0] != hash {
		t.Fatalf("doctor report=%+v", report)
	}
}

func isResourceError(err error, code string) bool {
	var target *ResourceLimitError
	return errors.As(err, &target) && target.Code == code
}
