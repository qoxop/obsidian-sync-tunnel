package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPutDeleteConflictAndChanges(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()

	firstData := []byte("first")
	first, changed, err := db.Put(ctx, "vault-a", "notes/hello.md", "desktop-a", 0, 1000, Hash(firstData), firstData)
	if err != nil || !changed {
		t.Fatalf("initial put: changed=%v err=%v", changed, err)
	}
	if first.Revision <= 0 || first.BlobHash != Hash(firstData) {
		t.Fatalf("unexpected first change: %+v", first)
	}

	retried, changed, err := db.Put(ctx, "vault-a", "notes/hello.md", "desktop-a", 0, 1000, Hash(firstData), firstData)
	if err != nil || changed || retried.Revision != first.Revision {
		t.Fatalf("idempotent retry: change=%+v changed=%v err=%v", retried, changed, err)
	}

	secondData := []byte("second")
	second, changed, err := db.Put(ctx, "vault-a", "notes/hello.md", "desktop-b", first.Revision, 2000, Hash(secondData), secondData)
	if err != nil || !changed || second.Revision <= first.Revision {
		t.Fatalf("update: change=%+v changed=%v err=%v", second, changed, err)
	}

	_, _, err = db.Put(ctx, "vault-a", "notes/hello.md", "desktop-a", first.Revision, 3000, Hash([]byte("third")), []byte("third"))
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Current.Revision != second.Revision {
		t.Fatalf("expected conflict at revision %d, got %v", second.Revision, err)
	}

	deleted, changed, err := db.Delete(ctx, "vault-a", "notes/hello.md", "desktop-b", second.Revision, 4000)
	if err != nil || !changed || !deleted.Deleted {
		t.Fatalf("delete: change=%+v changed=%v err=%v", deleted, changed, err)
	}

	retriedDelete, changed, err := db.Delete(ctx, "vault-a", "notes/hello.md", "desktop-a", first.Revision, 5000)
	if err != nil || changed || retriedDelete.Revision != deleted.Revision {
		t.Fatalf("idempotent delete: change=%+v changed=%v err=%v", retriedDelete, changed, err)
	}

	changes, hasMore, err := db.ListChanges(ctx, "vault-a", 0, 10)
	if err != nil || hasMore || len(changes) != 3 {
		t.Fatalf("changes: count=%d hasMore=%v err=%v", len(changes), hasMore, err)
	}
	if !changes[2].Deleted || changes[2].Revision != deleted.Revision {
		t.Fatalf("unexpected tombstone: %+v", changes[2])
	}

	blob, err := db.GetBlob(ctx, "vault-a", second.BlobHash)
	if err != nil || string(blob) != "second" {
		t.Fatalf("get blob: %q err=%v", blob, err)
	}
	if _, err := db.GetBlob(ctx, "vault-b", second.BlobHash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("blob must be isolated by vault, got %v", err)
	}
}

func TestListChangesPagination(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	for index, name := range []string{"a.md", "b.md", "c.md"} {
		data := []byte(name)
		if _, _, err := db.Put(ctx, "vault-a", name, "desktop", 0, int64(index+1), Hash(data), data); err != nil {
			t.Fatal(err)
		}
	}
	firstPage, hasMore, err := db.ListChanges(ctx, "vault-a", 0, 2)
	if err != nil || !hasMore || len(firstPage) != 2 {
		t.Fatalf("first page: count=%d hasMore=%v err=%v", len(firstPage), hasMore, err)
	}
	secondPage, hasMore, err := db.ListChanges(ctx, "vault-a", firstPage[1].Revision, 2)
	if err != nil || hasMore || len(secondPage) != 1 || secondPage[0].Path != "c.md" {
		t.Fatalf("second page: %+v hasMore=%v err=%v", secondPage, hasMore, err)
	}
}

func TestChunkStorageAndManifestAssembly(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	data := []byte("chunk content")
	hash := Hash(data)

	missing, err := db.MissingChunks(ctx, []string{hash})
	if err != nil || len(missing) != 1 || missing[0] != hash {
		t.Fatalf("missing before upload: %v err=%v", missing, err)
	}
	changed, err := db.PutChunk(ctx, hash, data)
	if err != nil || !changed {
		t.Fatalf("put chunk: changed=%v err=%v", changed, err)
	}
	changed, err = db.PutChunk(ctx, hash, data)
	if err != nil || changed {
		t.Fatalf("idempotent put chunk: changed=%v err=%v", changed, err)
	}
	missing, err = db.MissingChunks(ctx, []string{hash, hash})
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing after upload: %v err=%v", missing, err)
	}
	chunkPath := filepath.Join(db.blobDir, filepath.FromSlash(chunkRelativePath(hash)))
	if err := os.WriteFile(chunkPath, []byte("xxxxxxxxxxxxx"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing, err = db.MissingChunks(ctx, []string{hash})
	if err != nil || len(missing) != 1 {
		t.Fatalf("same-size corruption must be reported missing: %v err=%v", missing, err)
	}
	changed, err = db.PutChunk(ctx, hash, data)
	if err != nil || !changed {
		t.Fatalf("repair corrupt chunk: changed=%v err=%v", changed, err)
	}
	stored, err := db.GetChunk(ctx, hash)
	if err != nil || string(stored) != string(data) {
		t.Fatalf("get chunk: %q err=%v", stored, err)
	}
	assembled, err := db.AssembleChunks(ctx, hash, int64(len(data)), []ChunkRef{{Hash: hash, Size: int64(len(data))}})
	if err != nil || string(assembled) != string(data) {
		t.Fatalf("assemble chunks: %q err=%v", assembled, err)
	}
	var manifestCount int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM manifests WHERE content_hash=?`, hash).Scan(&manifestCount); err != nil || manifestCount != 1 {
		t.Fatalf("manifest count=%d err=%v", manifestCount, err)
	}
	file, _, err := db.Put(ctx, "vault-a", "chunk.bin", "desktop", 0, 1, hash, assembled)
	if err != nil || file.BlobHash != hash {
		t.Fatalf("record chunked file: change=%+v err=%v", file, err)
	}
	manifestSize, manifestChunks, err := db.GetManifest(ctx, "vault-a", hash)
	if err != nil || manifestSize != int64(len(data)) || len(manifestChunks) != 1 || manifestChunks[0].Hash != hash {
		t.Fatalf("get manifest: size=%d chunks=%+v err=%v", manifestSize, manifestChunks, err)
	}
	if _, _, err := db.GetManifest(ctx, "vault-b", hash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("manifest must be vault scoped, got %v", err)
	}
}

func TestOperationIDReturnsOriginalCommittedResult(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	firstOperation := "11111111-1111-4111-8111-111111111111"
	secondOperation := "22222222-2222-4222-8222-222222222222"
	firstData := []byte("first")
	first, changed, err := db.PutWithOperation(ctx, "vault-a", "note.md", "desktop", firstOperation, 0, 1000, Hash(firstData), firstData)
	if err != nil || !changed {
		t.Fatalf("first operation: change=%+v changed=%v err=%v", first, changed, err)
	}
	secondData := []byte("second")
	second, changed, err := db.PutWithOperation(ctx, "vault-a", "note.md", "desktop", secondOperation, first.Revision, 2000, Hash(secondData), secondData)
	if err != nil || !changed || second.Revision <= first.Revision {
		t.Fatalf("second operation: change=%+v changed=%v err=%v", second, changed, err)
	}

	retried, changed, err := db.PutWithOperation(ctx, "vault-a", "note.md", "desktop", firstOperation, 0, 1000, Hash(firstData), firstData)
	if err != nil || !changed || retried.Revision != first.Revision {
		t.Fatalf("operation retry: change=%+v changed=%v err=%v", retried, changed, err)
	}
	stored, storedChanged, found, err := db.GetOperation(ctx, "vault-a", "desktop", firstOperation)
	if err != nil || !found || !storedChanged || stored.Revision != first.Revision {
		t.Fatalf("stored operation: change=%+v changed=%v found=%v err=%v", stored, storedChanged, found, err)
	}
	if _, _, found, err := db.GetOperation(ctx, "vault-a", "other-device", firstOperation); err != nil || found {
		t.Fatalf("operation must be device scoped: found=%v err=%v", found, err)
	}

	_, _, err = db.PutWithOperation(ctx, "vault-a", "note.md", "desktop", firstOperation, 0, 1001, Hash(firstData), firstData)
	var reused *OperationReuseError
	if !errors.As(err, &reused) {
		t.Fatalf("expected operation reuse error, got %v", err)
	}
}

func TestRenameIsAtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()
	data := []byte("rename me")
	source, _, err := db.Put(ctx, "vault-a", "old.md", "desktop", 0, 1, Hash(data), data)
	if err != nil {
		t.Fatal(err)
	}
	operationID := "11111111-1111-4111-8111-111111111111"
	destination, related, changed, err := db.RenameWithOperation(ctx, "vault-a", "old.md", "folder/new.md", "desktop", operationID, source.Revision, 2)
	if err != nil || !changed || destination.Path != "folder/new.md" || destination.BlobHash != source.BlobHash {
		t.Fatalf("rename: destination=%+v related=%+v changed=%v err=%v", destination, related, changed, err)
	}
	if len(related) != 1 || related[0].Path != "old.md" || !related[0].Deleted || related[0].Revision >= destination.Revision {
		t.Fatalf("rename tombstone: %+v", related)
	}
	retried, retriedRelated, changed, err := db.RenameWithOperation(ctx, "vault-a", "old.md", "folder/new.md", "desktop", operationID, source.Revision, 2)
	if err != nil || !changed || retried.Revision != destination.Revision || len(retriedRelated) != 1 || retriedRelated[0].Revision != related[0].Revision {
		t.Fatalf("rename retry: destination=%+v related=%+v changed=%v err=%v", retried, retriedRelated, changed, err)
	}
	changes, _, err := db.ListChanges(ctx, "vault-a", source.Revision, 10)
	if err != nil || len(changes) != 2 || !changes[0].Deleted || changes[1].Path != "folder/new.md" {
		t.Fatalf("rename changes: %+v err=%v", changes, err)
	}
	stored, storedRelated, storedChanged, found, err := db.GetOperationDetails(ctx, "vault-a", "desktop", operationID)
	if err != nil || !found || !storedChanged || stored.Revision != destination.Revision || len(storedRelated) != 1 {
		t.Fatalf("stored rename: change=%+v related=%+v changed=%v found=%v err=%v", stored, storedRelated, storedChanged, found, err)
	}
}

func TestListSnapshotIsStableAtRevision(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	ctx := context.Background()

	a1Data := []byte("a1")
	a1, _, err := db.Put(ctx, "vault-a", "a.md", "desktop", 0, 1, Hash(a1Data), a1Data)
	if err != nil {
		t.Fatal(err)
	}
	bData := []byte("b")
	b, _, err := db.Put(ctx, "vault-a", "b.md", "desktop", 0, 2, Hash(bData), bData)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRevision := b.Revision

	a2Data := []byte("a2")
	if _, _, err := db.Put(ctx, "vault-a", "a.md", "desktop", a1.Revision, 3, Hash(a2Data), a2Data); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.Delete(ctx, "vault-a", "b.md", "desktop", b.Revision, 4); err != nil {
		t.Fatal(err)
	}
	cData := []byte("c")
	if _, _, err := db.Put(ctx, "vault-a", "c.md", "desktop", 0, 5, Hash(cData), cData); err != nil {
		t.Fatal(err)
	}

	firstPage, hasMore, err := db.ListSnapshot(ctx, "vault-a", snapshotRevision, "", 1)
	if err != nil || !hasMore || len(firstPage) != 1 || firstPage[0].Path != "a.md" || firstPage[0].BlobHash != Hash(a1Data) {
		t.Fatalf("first snapshot page: %+v hasMore=%v err=%v", firstPage, hasMore, err)
	}
	secondPage, hasMore, err := db.ListSnapshot(ctx, "vault-a", snapshotRevision, firstPage[0].Path, 10)
	if err != nil || hasMore || len(secondPage) != 1 || secondPage[0].Path != "b.md" || secondPage[0].Deleted {
		t.Fatalf("second snapshot page: %+v hasMore=%v err=%v", secondPage, hasMore, err)
	}

	latest, hasMore, err := db.ListSnapshot(ctx, "vault-a", 1<<62, "", 10)
	if err != nil || hasMore || len(latest) != 3 {
		t.Fatalf("latest snapshot: %+v hasMore=%v err=%v", latest, hasMore, err)
	}
	if latest[1].Path != "b.md" || !latest[1].Deleted {
		t.Fatalf("latest tombstone missing: %+v", latest)
	}
}

func TestSchemaVersion(t *testing.T) {
	t.Parallel()
	db := openTestStore(t)
	version, err := db.SchemaVersion(context.Background())
	if err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

func TestSchemaMigrationFromVersionOneAddsOperations(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range migrations[0] {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var table string
	if err := db.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='operations'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "operations" {
		t.Fatalf("unexpected table %q", table)
	}
	if err := db.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='chunks'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "chunks" {
		t.Fatalf("unexpected table %q", table)
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()
	valid, err := NormalizePath(`folder\note.md`)
	if err != nil || valid != "folder/note.md" {
		t.Fatalf("normalize valid path: %q %v", valid, err)
	}
	for _, value := range []string{"", "/absolute", "../escape", "folder//file", "folder/./file", "folder/../file"} {
		if _, err := NormalizePath(value); err == nil {
			t.Errorf("expected %q to be rejected", value)
		}
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
