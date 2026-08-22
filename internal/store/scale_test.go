package store

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestScaleTenThousandFiles is opt-in so the normal suite remains fast. Run:
//
//	$env:OBSIDIAN_SYNC_SCALE_TEST='1'; go test ./internal/store -run TestScaleTenThousandFiles -count=1 -v
func TestScaleTenThousandFiles(t *testing.T) {
	if os.Getenv("OBSIDIAN_SYNC_SCALE_TEST") != "1" {
		t.Skip("set OBSIDIAN_SYNC_SCALE_TEST=1 to run the 10,000-file scale test")
	}
	ctx := context.Background()
	db := openTestStore(t)
	const fileCount = 10_000
	for index := 0; index < fileCount; index++ {
		path := fmt.Sprintf("scale/%03d/file-%05d.md", index%100, index)
		data := []byte(fmt.Sprintf("file %d", index))
		if _, changed, err := db.Put(ctx, "vault-a", path, "scale-device", 0, int64(index+1), Hash(data), data); err != nil || !changed {
			t.Fatalf("put %d: changed=%v err=%v", index, changed, err)
		}
	}

	var cursor string
	seen := 0
	for {
		page, more, err := db.ListSnapshot(ctx, "vault-a", fileCount, cursor, 317)
		if err != nil {
			t.Fatal(err)
		}
		seen += len(page)
		if !more {
			break
		}
		cursor = page[len(page)-1].Path
	}
	if seen != fileCount {
		t.Fatalf("snapshot returned %d files, want %d", seen, fileCount)
	}
	changes, more, err := db.ListChanges(ctx, "vault-a", fileCount-250, 250)
	if err != nil || more || len(changes) != 250 {
		t.Fatalf("tail changes=%d more=%v err=%v", len(changes), more, err)
	}
}
