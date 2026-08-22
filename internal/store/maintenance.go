package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type VaultPath struct {
	VaultID string `json:"vault_id"`
	Path    string `json:"path"`
}

type VaultWatermark struct {
	VaultID  string `json:"vault_id"`
	Revision int64  `json:"revision"`
}

type GCPlan struct {
	ID              string           `json:"id"`
	Hash            string           `json:"hash"`
	CreatedAt       int64            `json:"created_at"`
	RetentionDays   int              `json:"retention_days"`
	KeepVersions    int              `json:"keep_versions"`
	SafeRevision    int64            `json:"safe_revision"`
	VaultWatermarks []VaultWatermark `json:"vault_watermarks"`
	ChangeRevisions []int64          `json:"change_revisions"`
	DeletedPaths    []VaultPath      `json:"deleted_paths"`
	BlobHashes      []string         `json:"blob_hashes"`
	ManifestHashes  []string         `json:"manifest_hashes"`
	ChunkHashes     []string         `json:"chunk_hashes"`
	OperationCutoff int64            `json:"operation_cutoff"`
	EstimatedBytes  int64            `json:"estimated_bytes"`
}

type GCResult struct {
	PlanID            string `json:"plan_id"`
	ChangesDeleted    int64  `json:"changes_deleted"`
	PathsDeleted      int64  `json:"paths_deleted"`
	BlobsDeleted      int64  `json:"blobs_deleted"`
	ManifestsDeleted  int64  `json:"manifests_deleted"`
	ChunksDeleted     int64  `json:"chunks_deleted"`
	OperationsDeleted int64  `json:"operations_deleted"`
	BytesReclaimed    int64  `json:"bytes_reclaimed"`
}

type DoctorReport struct {
	OK                 bool     `json:"ok"`
	Integrity          string   `json:"integrity"`
	MissingChunkHashes []string `json:"missing_chunk_hashes"`
	CorruptChunkHashes []string `json:"corrupt_chunk_hashes"`
	OrphanChunkFiles   []string `json:"orphan_chunk_files"`
}

func (s *Store) BuildGCPlan(ctx context.Context, retentionDays, keepVersions int) (GCPlan, error) {
	if retentionDays < 1 || keepVersions < 1 {
		return GCPlan{}, errors.New("retention_days and keep_versions must be positive")
	}
	s.maintenance.Lock()
	defer s.maintenance.Unlock()

	planID, err := randomID("gc")
	if err != nil {
		return GCPlan{}, err
	}
	plan := GCPlan{ID: planID, CreatedAt: time.Now().UnixMilli(), RetentionDays: retentionDays, KeepVersions: keepVersions}
	watermarks := make(map[string]int64)
	watermarkRows, err := s.db.QueryContext(ctx, `SELECT vault_id, MIN(last_ack_revision) FROM devices WHERE status='active' GROUP BY vault_id HAVING MIN(last_ack_revision)>0 ORDER BY vault_id`)
	if err != nil {
		return GCPlan{}, err
	}
	for watermarkRows.Next() {
		var item VaultWatermark
		if err := watermarkRows.Scan(&item.VaultID, &item.Revision); err != nil {
			watermarkRows.Close()
			return GCPlan{}, err
		}
		watermarks[item.VaultID] = item.Revision
		plan.VaultWatermarks = append(plan.VaultWatermarks, item)
		if plan.SafeRevision == 0 || item.Revision < plan.SafeRevision {
			plan.SafeRevision = item.Revision
		}
	}
	watermarkRows.Close()
	if len(watermarks) == 0 {
		return s.saveGCPlan(ctx, plan)
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()

	rows, err := s.db.QueryContext(ctx, `WITH watermarks AS (
		SELECT vault_id, MIN(last_ack_revision) AS safe_revision FROM devices WHERE status='active' GROUP BY vault_id HAVING MIN(last_ack_revision)>0
	), ranked AS (
		SELECT seq, vault_id, path, deleted, created_at,
		ROW_NUMBER() OVER(PARTITION BY vault_id, path ORDER BY seq DESC) AS position
		FROM changes
	) SELECT ranked.seq, ranked.vault_id, ranked.path, ranked.deleted, ranked.position FROM ranked
	JOIN watermarks ON watermarks.vault_id=ranked.vault_id
	WHERE ranked.seq<=watermarks.safe_revision AND ranked.created_at<? AND (ranked.position>? OR (ranked.position=1 AND ranked.deleted=1)) ORDER BY ranked.seq`, cutoff, keepVersions)
	if err != nil {
		return GCPlan{}, err
	}
	candidates := make(map[int64]bool)
	deletedPathSet := make(map[string]VaultPath)
	for rows.Next() {
		var revision, position int64
		var vaultID, path string
		var deleted bool
		if err := rows.Scan(&revision, &vaultID, &path, &deleted, &position); err != nil {
			rows.Close()
			return GCPlan{}, err
		}
		candidates[revision] = true
		if position == 1 && deleted {
			deletedPathSet[vaultID+"\x00"+path] = VaultPath{VaultID: vaultID, Path: path}
		}
	}
	if err := rows.Close(); err != nil {
		return GCPlan{}, err
	}
	// Once a safely acknowledged tombstone is removed, every older version of
	// that deleted path can be removed in the same deterministic plan.
	for _, item := range deletedPathSet {
		pathRows, err := s.db.QueryContext(ctx, `SELECT seq FROM changes WHERE vault_id=? AND path=? AND seq<=?`, item.VaultID, item.Path, watermarks[item.VaultID])
		if err != nil {
			return GCPlan{}, err
		}
		for pathRows.Next() {
			var revision int64
			if err := pathRows.Scan(&revision); err != nil {
				pathRows.Close()
				return GCPlan{}, err
			}
			candidates[revision] = true
		}
		pathRows.Close()
	}
	for revision := range candidates {
		plan.ChangeRevisions = append(plan.ChangeRevisions, revision)
	}
	sort.Slice(plan.ChangeRevisions, func(i, j int) bool { return plan.ChangeRevisions[i] < plan.ChangeRevisions[j] })
	for _, item := range deletedPathSet {
		plan.DeletedPaths = append(plan.DeletedPaths, item)
	}
	sort.Slice(plan.DeletedPaths, func(i, j int) bool {
		if plan.DeletedPaths[i].VaultID == plan.DeletedPaths[j].VaultID {
			return plan.DeletedPaths[i].Path < plan.DeletedPaths[j].Path
		}
		return plan.DeletedPaths[i].VaultID < plan.DeletedPaths[j].VaultID
	})

	refs := make(map[string][]int64)
	blobSizes := make(map[string]int64)
	rows, err = s.db.QueryContext(ctx, `SELECT c.seq, c.blob_hash, b.size FROM changes c JOIN blobs b ON b.hash=c.blob_hash WHERE c.blob_hash IS NOT NULL`)
	if err != nil {
		return GCPlan{}, err
	}
	for rows.Next() {
		var revision, size int64
		var hash string
		if err := rows.Scan(&revision, &hash, &size); err != nil {
			rows.Close()
			return GCPlan{}, err
		}
		refs[hash] = append(refs[hash], revision)
		blobSizes[hash] = size
	}
	rows.Close()
	for hash, revisions := range refs {
		remove := len(revisions) > 0
		for _, revision := range revisions {
			if !candidates[revision] {
				remove = false
				break
			}
		}
		if remove {
			plan.BlobHashes = append(plan.BlobHashes, hash)
			plan.EstimatedBytes += blobSizes[hash]
		}
	}
	sort.Strings(plan.BlobHashes)
	plan.ManifestHashes = append(plan.ManifestHashes, plan.BlobHashes...)

	removedManifests := make(map[string]bool)
	for _, hash := range plan.ManifestHashes {
		removedManifests[hash] = true
	}
	chunkRefs := make(map[string][]string)
	chunkSizes := make(map[string]int64)
	rows, err = s.db.QueryContext(ctx, `SELECT content_hash, chunks_json FROM manifests`)
	if err != nil {
		return GCPlan{}, err
	}
	for rows.Next() {
		var contentHash, encoded string
		if err := rows.Scan(&contentHash, &encoded); err != nil {
			rows.Close()
			return GCPlan{}, err
		}
		var chunks []ChunkRef
		if err := json.Unmarshal([]byte(encoded), &chunks); err != nil {
			rows.Close()
			return GCPlan{}, err
		}
		for _, chunk := range chunks {
			chunkRefs[chunk.Hash] = append(chunkRefs[chunk.Hash], contentHash)
			chunkSizes[chunk.Hash] = chunk.Size
		}
	}
	rows.Close()
	for hash, manifests := range chunkRefs {
		remove := true
		for _, manifest := range manifests {
			if !removedManifests[manifest] {
				remove = false
				break
			}
		}
		if remove {
			plan.ChunkHashes = append(plan.ChunkHashes, hash)
			plan.EstimatedBytes += chunkSizes[hash]
		}
	}
	orphanCutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	rows, err = s.db.QueryContext(ctx, `SELECT hash, size FROM chunks WHERE created_at<? ORDER BY hash`, orphanCutoff)
	if err != nil {
		return GCPlan{}, err
	}
	plannedChunks := make(map[string]bool)
	for _, hash := range plan.ChunkHashes {
		plannedChunks[hash] = true
	}
	for rows.Next() {
		var hash string
		var size int64
		if err := rows.Scan(&hash, &size); err != nil {
			rows.Close()
			return GCPlan{}, err
		}
		if len(chunkRefs[hash]) == 0 && !plannedChunks[hash] {
			plan.ChunkHashes = append(plan.ChunkHashes, hash)
			plan.EstimatedBytes += size
		}
	}
	rows.Close()
	sort.Strings(plan.ChunkHashes)
	plan.OperationCutoff = cutoff
	return s.saveGCPlan(ctx, plan)
}

func (s *Store) saveGCPlan(ctx context.Context, plan GCPlan) (GCPlan, error) {
	plan.Hash = ""
	encoded, err := json.Marshal(plan)
	if err != nil {
		return GCPlan{}, err
	}
	plan.Hash = Hash(encoded)
	encoded, err = json.Marshal(plan)
	if err != nil {
		return GCPlan{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO gc_plans(id, plan_hash, plan_json, status, created_at) VALUES(?, ?, ?, 'pending', ?)`, plan.ID, plan.Hash, string(encoded), plan.CreatedAt)
	if err != nil {
		return GCPlan{}, err
	}
	_ = s.RecordAudit(ctx, "gc.planned", "", "", "admin", "", map[string]any{"plan_id": plan.ID, "plan_hash": plan.Hash})
	return plan, nil
}

func (s *Store) ExecuteGCPlan(ctx context.Context, planID, expectedHash string) (GCResult, error) {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	var encoded, storedHash, status string
	err := s.db.QueryRowContext(ctx, `SELECT plan_json, plan_hash, status FROM gc_plans WHERE id=?`, planID).Scan(&encoded, &storedHash, &status)
	if err != nil {
		return GCResult{}, err
	}
	if status != "pending" || storedHash != expectedHash {
		return GCResult{}, errors.New("GC plan is not pending or hash does not match")
	}
	var plan GCPlan
	if err := json.Unmarshal([]byte(encoded), &plan); err != nil {
		return GCResult{}, err
	}
	providedHash := plan.Hash
	plan.Hash = ""
	canonical, _ := json.Marshal(plan)
	if Hash(canonical) != providedHash || providedHash != storedHash {
		return GCResult{}, errors.New("GC plan integrity check failed")
	}
	result := GCResult{PlanID: plan.ID, BytesReclaimed: plan.EstimatedBytes}
	var removedChunks []string
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GCResult{}, err
	}
	defer tx.Rollback()
	for _, revision := range plan.ChangeRevisions {
		outcome, err := tx.ExecContext(ctx, `DELETE FROM changes WHERE seq=?`, revision)
		if err != nil {
			return GCResult{}, err
		}
		count, _ := outcome.RowsAffected()
		result.ChangesDeleted += count
	}
	for _, item := range plan.DeletedPaths {
		outcome, err := tx.ExecContext(ctx, `DELETE FROM files WHERE vault_id=? AND path=? AND deleted=1`, item.VaultID, item.Path)
		if err != nil {
			return GCResult{}, err
		}
		count, _ := outcome.RowsAffected()
		result.PathsDeleted += count
	}
	for _, hash := range plan.BlobHashes {
		outcome, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE hash=? AND NOT EXISTS(SELECT 1 FROM changes WHERE blob_hash=?)`, hash, hash)
		if err != nil {
			return GCResult{}, err
		}
		count, _ := outcome.RowsAffected()
		result.BlobsDeleted += count
	}
	for _, hash := range plan.ManifestHashes {
		outcome, err := tx.ExecContext(ctx, `DELETE FROM manifests WHERE content_hash=? AND NOT EXISTS(SELECT 1 FROM changes WHERE blob_hash=?)`, hash, hash)
		if err != nil {
			return GCResult{}, err
		}
		count, _ := outcome.RowsAffected()
		result.ManifestsDeleted += count
	}
	for _, hash := range plan.ChunkHashes {
		outcome, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE hash=? AND NOT EXISTS(
			SELECT 1 FROM manifests m JOIN json_each(m.chunks_json) part WHERE json_extract(part.value, '$.hash')=?
		)`, hash, hash)
		if err != nil {
			return GCResult{}, err
		}
		count, _ := outcome.RowsAffected()
		result.ChunksDeleted += count
		if count == 1 {
			removedChunks = append(removedChunks, hash)
		}
	}
	if plan.OperationCutoff > 0 {
		outcome, err := tx.ExecContext(ctx, `DELETE FROM operations WHERE created_at<?`, plan.OperationCutoff)
		if err != nil {
			return GCResult{}, err
		}
		result.OperationsDeleted, _ = outcome.RowsAffected()
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pairing_codes WHERE expires_at<?`, time.Now().UnixMilli()); err != nil {
		return GCResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE gc_plans SET status='executed', executed_at=? WHERE id=? AND status='pending'`, time.Now().UnixMilli(), plan.ID); err != nil {
		return GCResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return GCResult{}, err
	}
	for _, hash := range removedChunks {
		_ = os.Remove(filepath.Join(s.blobDir, filepath.FromSlash(chunkRelativePath(hash))))
	}
	_ = s.RecordAudit(ctx, "gc.executed", "", "", "admin", "", map[string]any{"plan_id": plan.ID, "plan_hash": plan.Hash})
	return result, nil
}

func (s *Store) Doctor(ctx context.Context) (DoctorReport, error) {
	report := DoctorReport{OK: true}
	integrityDB, err := sql.Open("sqlite", s.databasePath)
	if err != nil {
		return DoctorReport{}, err
	}
	integrityDB.SetMaxOpenConns(1)
	integrityDB.SetMaxIdleConns(0)
	defer integrityDB.Close()
	if _, err := integrityDB.ExecContext(ctx, `PRAGMA query_only = ON`); err != nil {
		return DoctorReport{}, err
	}
	if _, err := integrityDB.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return DoctorReport{}, err
	}
	if err := integrityDB.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&report.Integrity); err != nil {
		return DoctorReport{}, err
	}
	if report.Integrity != "ok" {
		report.OK = false
	}
	type chunkTarget struct {
		hash       string
		path       string
		unsafePath bool
	}
	targets := make([]chunkTarget, 0)
	known := make(map[string]bool)
	rows, err := s.db.QueryContext(ctx, `SELECT hash, relative_path FROM chunks ORDER BY hash`)
	if err != nil {
		return DoctorReport{}, err
	}
	for rows.Next() {
		var hash, relative string
		if err := rows.Scan(&hash, &relative); err != nil {
			rows.Close()
			return DoctorReport{}, err
		}
		path := filepath.Clean(filepath.Join(s.blobDir, filepath.FromSlash(relative)))
		if path == filepath.Clean(s.blobDir) || !pathWithin(path, s.blobDir) {
			targets = append(targets, chunkTarget{hash: hash, unsafePath: true})
			continue
		}
		known[path] = true
		targets = append(targets, chunkTarget{hash: hash, path: path})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return DoctorReport{}, err
	}
	if err := rows.Close(); err != nil {
		return DoctorReport{}, err
	}

	type chunkResult struct {
		missing bool
		corrupt bool
		err     error
	}
	results := make([]chunkResult, len(targets))
	workers := min(runtime.GOMAXPROCS(0), 4, len(targets))
	if workers > 0 {
		jobs := make(chan int)
		var group sync.WaitGroup
		group.Add(workers)
		for range workers {
			go func() {
				defer group.Done()
				for index := range jobs {
					if targets[index].unsafePath {
						results[index].corrupt = true
						continue
					}
					actual, hashErr := hashFile(targets[index].path)
					if errors.Is(hashErr, os.ErrNotExist) {
						results[index].missing = true
						continue
					}
					if hashErr != nil {
						results[index].err = hashErr
						continue
					}
					results[index].corrupt = actual != targets[index].hash
				}
			}()
		}
		for index := range targets {
			select {
			case jobs <- index:
			case <-ctx.Done():
				close(jobs)
				group.Wait()
				return DoctorReport{}, ctx.Err()
			}
		}
		close(jobs)
		group.Wait()
	}
	for index, result := range results {
		if result.err != nil {
			return DoctorReport{}, result.err
		}
		if result.missing {
			report.MissingChunkHashes = append(report.MissingChunkHashes, targets[index].hash)
			report.OK = false
		}
		if result.corrupt {
			report.CorruptChunkHashes = append(report.CorruptChunkHashes, targets[index].hash)
			report.OK = false
		}
	}
	root := filepath.Join(s.blobDir, "chunks")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if !known[filepath.Clean(path)] {
			relative, _ := filepath.Rel(s.blobDir, path)
			report.OrphanChunkFiles = append(report.OrphanChunkFiles, filepath.ToSlash(relative))
		}
		return nil
	})
	return report, nil
}

type BackupManifest struct {
	FormatVersion int               `json:"format_version"`
	CreatedAt     int64             `json:"created_at"`
	SchemaVersion int               `json:"schema_version"`
	Files         map[string]string `json:"files"`
}

type BackupRun struct {
	ID           string `json:"id"`
	Destination  string `json:"destination"`
	Status       string `json:"status"`
	ManifestHash string `json:"manifest_hash,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	CompletedAt  int64  `json:"completed_at,omitempty"`
	ErrorText    string `json:"error_text,omitempty"`
}

func (s *Store) Backup(ctx context.Context, destination string) (BackupManifest, error) {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	abs, err := filepath.Abs(destination)
	if err != nil {
		return BackupManifest{}, err
	}
	dataRoot, _ := filepath.Abs(filepath.Dir(s.databasePath))
	if pathWithin(abs, dataRoot) {
		return BackupManifest{}, errors.New("backup destination must be outside the live data directory")
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return BackupManifest{}, errors.New("backup destination must be empty")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return BackupManifest{}, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return BackupManifest{}, err
	}
	dbTarget := filepath.Join(abs, "sync.db")
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dbTarget); err != nil {
		return BackupManifest{}, fmt.Errorf("create SQLite snapshot: %w", err)
	}
	if err := copyTree(s.blobDir, filepath.Join(abs, "blobs")); err != nil {
		return BackupManifest{}, err
	}
	schema, err := s.SchemaVersion(ctx)
	if err != nil {
		return BackupManifest{}, err
	}
	manifest := BackupManifest{FormatVersion: 1, CreatedAt: time.Now().UnixMilli(), SchemaVersion: schema, Files: make(map[string]string)}
	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || entry.Name() == "backup.json" {
			return walkErr
		}
		relative, _ := filepath.Rel(abs, path)
		hash, err := hashFile(path)
		if err != nil {
			return err
		}
		manifest.Files[filepath.ToSlash(relative)] = hash
		return nil
	})
	if err != nil {
		return BackupManifest{}, err
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BackupManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(abs, "backup.json"), encoded, 0o600); err != nil {
		return BackupManifest{}, err
	}
	runID, _ := randomID("backup")
	_, _ = s.db.ExecContext(ctx, `INSERT INTO backup_runs(id, destination, status, manifest_hash, created_at, completed_at)
		VALUES(?, ?, 'completed', ?, ?, ?)`, runID, abs, Hash(encoded), manifest.CreatedAt, time.Now().UnixMilli())
	_ = s.RecordAudit(ctx, "backup.completed", "", "", "admin", "", map[string]any{"manifest_hash": Hash(encoded), "file_count": len(manifest.Files)})
	return manifest, nil
}

func (s *Store) ListBackupRuns(ctx context.Context, limit int) ([]BackupRun, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("backup limit must be between 1 and 1000")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, destination, status, COALESCE(manifest_hash,''), created_at, COALESCE(completed_at,0), COALESCE(error_text,'')
		FROM backup_runs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]BackupRun, 0)
	for rows.Next() {
		var run BackupRun
		if err := rows.Scan(&run.ID, &run.Destination, &run.Status, &run.ManifestHash, &run.CreatedAt, &run.CompletedAt, &run.ErrorText); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (s *Store) VerifyBackupRun(ctx context.Context, id string) (string, BackupManifest, error) {
	var destination string
	err := s.db.QueryRowContext(ctx, `SELECT destination FROM backup_runs WHERE id=? AND status='completed'`, id).Scan(&destination)
	if err != nil {
		return "", BackupManifest{}, err
	}
	manifest, err := VerifyBackup(destination)
	return destination, manifest, err
}

func VerifyBackup(directory string) (BackupManifest, error) {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return BackupManifest{}, err
	}
	encoded, err := os.ReadFile(filepath.Join(abs, "backup.json"))
	if err != nil {
		return BackupManifest{}, err
	}
	var manifest BackupManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return BackupManifest{}, err
	}
	if manifest.FormatVersion != 1 || manifest.SchemaVersion < 1 {
		return BackupManifest{}, errors.New("unsupported backup manifest")
	}
	actualFiles := make(map[string]bool)
	if err := filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symbolic links are not allowed in backup data")
		}
		if entry.IsDir() || entry.Name() == "backup.json" {
			return nil
		}
		relative, _ := filepath.Rel(abs, path)
		actualFiles[filepath.ToSlash(relative)] = true
		return nil
	}); err != nil {
		return BackupManifest{}, err
	}
	if len(actualFiles) != len(manifest.Files) {
		return BackupManifest{}, errors.New("backup file set does not match manifest")
	}
	for relative, expected := range manifest.Files {
		clean := filepath.Clean(filepath.FromSlash(relative))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return BackupManifest{}, errors.New("backup manifest contains an unsafe path")
		}
		actual, err := hashFile(filepath.Join(abs, clean))
		if err != nil {
			return BackupManifest{}, err
		}
		if actual != expected {
			return BackupManifest{}, fmt.Errorf("backup hash mismatch: %s", relative)
		}
		if !actualFiles[filepath.ToSlash(relative)] {
			return BackupManifest{}, fmt.Errorf("backup file is missing: %s", relative)
		}
	}
	check, err := sql.Open("sqlite", filepath.Join(abs, "sync.db"))
	if err != nil {
		return BackupManifest{}, err
	}
	defer check.Close()
	var integrity string
	if err := check.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return BackupManifest{}, errors.New("backup SQLite integrity check failed")
	}
	return manifest, nil
}

func RestoreBackup(source, target string) error {
	if _, err := VerifyBackup(source); err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	sourceAbs, _ := filepath.Abs(source)
	if pathWithin(targetAbs, sourceAbs) {
		return errors.New("restore target must be outside the backup source")
	}
	if entries, err := os.ReadDir(targetAbs); err == nil && len(entries) > 0 {
		return errors.New("restore target must be empty")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return copyTree(source, targetAbs)
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symbolic links are not allowed in backup data")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
