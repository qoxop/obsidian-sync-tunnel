package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

func TestDoctorResultCacheCoalescesAndCaches(t *testing.T) {
	cache := newDoctorResultCache(time.Minute)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	run := func(context.Context) (store.DoctorReport, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return store.DoctorReport{OK: true, Integrity: "ok"}, nil
	}

	const clients = 8
	results := make(chan bool, clients)
	var group sync.WaitGroup
	group.Add(clients)
	for range clients {
		go func() {
			defer group.Done()
			report, _, err := cache.Get(context.Background(), run)
			results <- err == nil && report.OK
		}()
	}
	<-started
	close(release)
	group.Wait()
	close(results)
	for ok := range results {
		if !ok {
			t.Fatal("coalesced result failed")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("doctor calls=%d", calls.Load())
	}
	_, cached, err := cache.Get(context.Background(), run)
	if err != nil || !cached || calls.Load() != 1 {
		t.Fatalf("cached=%v calls=%d err=%v", cached, calls.Load(), err)
	}
}

func TestDoctorResultCacheDoesNotCacheErrors(t *testing.T) {
	cache := newDoctorResultCache(time.Minute)
	var calls atomic.Int32
	run := func(context.Context) (store.DoctorReport, error) {
		if calls.Add(1) == 1 {
			return store.DoctorReport{}, errors.New("failed")
		}
		return store.DoctorReport{OK: true, Integrity: "ok"}, nil
	}
	if _, _, err := cache.Get(context.Background(), run); err == nil {
		t.Fatal("doctor error was hidden")
	}
	report, cached, err := cache.Get(context.Background(), run)
	if err != nil || cached || !report.OK || calls.Load() != 2 {
		t.Fatalf("report=%+v cached=%v calls=%d err=%v", report, cached, calls.Load(), err)
	}
}

func TestDoctorHandlerCachesSuccessfulReport(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	admin := NewAdmin(db, "", AdminOptions{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	first := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/v1/doctor", nil))
	if first.Code != http.StatusOK || first.Header().Get("X-Sync-Doctor-Cache") != "MISS" {
		t.Fatalf("first doctor status=%d cache=%q body=%s", first.Code, first.Header().Get("X-Sync-Doctor-Cache"), first.Body)
	}
	second := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/v1/doctor", nil))
	if second.Code != http.StatusOK || second.Header().Get("X-Sync-Doctor-Cache") != "HIT" {
		t.Fatalf("second doctor status=%d cache=%q body=%s", second.Code, second.Header().Get("X-Sync-Doctor-Cache"), second.Body)
	}
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("cached doctor response changed: first=%s second=%s", first.Body, second.Body)
	}
}
