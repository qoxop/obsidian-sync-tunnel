package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

type stateModelFile struct {
	revision int64
	hash     string
	data     []byte
	deleted  bool
}

// TestDeterministicMultiDeviceStateMachine exercises a reproducible sequence
// of writes, deletes, idempotent retries and stale-revision conflicts. The
// in-memory model is independent of SQLite and is compared with snapshots and
// blobs throughout the run.
func TestDeterministicMultiDeviceStateMachine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestStore(t)
	rng := rand.New(rand.NewSource(20260819))
	devices := []string{"windows-a", "windows-b", "mac-a", "android-a", "ios-a"}
	paths := make([]string, 24)
	for i := range paths {
		paths[i] = fmt.Sprintf("folder-%02d/note-%02d.md", i%4, i)
	}
	model := make(map[string]stateModelFile)
	var successfulChanges int64

	for step := 1; step <= 2500; step++ {
		path := paths[rng.Intn(len(paths))]
		device := devices[rng.Intn(len(devices))]
		current := model[path]
		choice := rng.Intn(100)
		switch {
		case choice < 65:
			data := []byte(fmt.Sprintf("seed=20260819 step=%d device=%s value=%d", step, device, rng.Int63()))
			change, changed, err := db.Put(ctx, "vault-a", path, device, current.revision, int64(step), Hash(data), data)
			if err != nil || !changed {
				t.Fatalf("step %d put %s: changed=%v err=%v", step, path, changed, err)
			}
			successfulChanges++
			if change.Revision != successfulChanges {
				t.Fatalf("step %d revision=%d want=%d", step, change.Revision, successfulChanges)
			}
			model[path] = stateModelFile{revision: change.Revision, hash: Hash(data), data: data}

		case choice < 85:
			change, changed, err := db.Delete(ctx, "vault-a", path, device, current.revision, int64(step))
			if current.revision == 0 || current.deleted {
				if err != nil || changed {
					t.Fatalf("step %d idempotent delete %s: changed=%v err=%v", step, path, changed, err)
				}
				continue
			}
			if err != nil || !changed || !change.Deleted {
				t.Fatalf("step %d delete %s: change=%+v changed=%v err=%v", step, path, change, changed, err)
			}
			successfulChanges++
			model[path] = stateModelFile{revision: change.Revision, deleted: true}

		default:
			if current.revision == 0 {
				continue
			}
			staleBase := current.revision - 1
			data := []byte(fmt.Sprintf("stale step=%d", step))
			_, _, err := db.Put(ctx, "vault-a", path, device, staleBase, int64(step), Hash(data), data)
			var conflict *ConflictError
			if !errors.As(err, &conflict) || conflict.Current.Revision != current.revision {
				t.Fatalf("step %d stale put %s: current=%d err=%v", step, path, current.revision, err)
			}
		}

		if step%100 == 0 {
			assertStoreMatchesModel(t, ctx, db, model, successfulChanges)
		}
	}
	assertStoreMatchesModel(t, ctx, db, model, successfulChanges)
}

func assertStoreMatchesModel(t *testing.T, ctx context.Context, db *Store, model map[string]stateModelFile, latest int64) {
	t.Helper()
	actual, hasMore, err := db.ListSnapshot(ctx, "vault-a", latest, "", 1000)
	if err != nil || hasMore || len(actual) != len(model) {
		t.Fatalf("snapshot: files=%d want=%d hasMore=%v err=%v", len(actual), len(model), hasMore, err)
	}
	for _, item := range actual {
		expected, ok := model[item.Path]
		if !ok || item.Revision != expected.revision || item.Deleted != expected.deleted || item.BlobHash != expected.hash {
			t.Fatalf("snapshot mismatch path=%s actual=%+v expected=%+v", item.Path, item, expected)
		}
		if !item.Deleted {
			blob, err := db.GetBlob(ctx, "vault-a", item.BlobHash)
			if err != nil || string(blob) != string(expected.data) {
				t.Fatalf("blob mismatch path=%s err=%v", item.Path, err)
			}
		}
	}
	revision, err := db.LatestRevision(ctx, "vault-a")
	if err != nil || revision != latest {
		t.Fatalf("latest revision=%d want=%d err=%v", revision, latest, err)
	}
}
