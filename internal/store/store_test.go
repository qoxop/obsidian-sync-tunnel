package store

import (
	"context"
	"database/sql"
	"errors"
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
