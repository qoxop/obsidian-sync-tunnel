package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type HistoryPage struct {
	Versions []Change `json:"versions"`
	Cursor   int64    `json:"cursor"`
	HasMore  bool     `json:"has_more"`
}

func (s *Store) ListHistory(ctx context.Context, vaultID, filePath string, before int64, limit int, deletedOnly bool) (HistoryPage, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return HistoryPage{}, err
	}
	if filePath != "" {
		var err error
		filePath, err = NormalizePath(filePath)
		if err != nil {
			return HistoryPage{}, err
		}
	}
	if before < 0 || limit < 1 || limit > 250 {
		return HistoryPage{}, errors.New("history cursor or limit is invalid")
	}
	query := `SELECT seq, path, blob_hash, size, modified_at, deleted, device_id, operation_kind, COALESCE(restored_from_revision,0)
		FROM changes WHERE vault_id=? AND (?='' OR path=?) AND (?=0 OR seq<?) AND (?=0 OR deleted=1)
		ORDER BY seq DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, query, vaultID, filePath, filePath, before, before, deletedOnly, limit+1)
	if err != nil {
		return HistoryPage{}, err
	}
	defer rows.Close()
	page := HistoryPage{Versions: make([]Change, 0, limit)}
	for rows.Next() {
		var change Change
		var hash sql.NullString
		if err := rows.Scan(&change.Revision, &change.Path, &hash, &change.Size, &change.ModifiedAt, &change.Deleted, &change.DeviceID, &change.OperationKind, &change.RestoredFromRevision); err != nil {
			return HistoryPage{}, err
		}
		change.BlobHash = hash.String
		page.Versions = append(page.Versions, change)
	}
	if err := rows.Err(); err != nil {
		return HistoryPage{}, err
	}
	if len(page.Versions) > limit {
		page.HasMore = true
		page.Versions = page.Versions[:limit]
	}
	if len(page.Versions) > 0 {
		page.Cursor = page.Versions[len(page.Versions)-1].Revision
	}
	return page, nil
}

func (s *Store) RestoreVersion(ctx context.Context, vaultID, filePath, deviceID, operationID string, sourceRevision, baseRevision, modifiedAt int64) (Change, bool, error) {
	if sourceRevision <= 0 {
		return Change{}, false, errors.New("source revision must be positive")
	}
	filePath, err := NormalizePath(filePath)
	if err != nil {
		return Change{}, false, err
	}
	var source Change
	var hash sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT seq, path, blob_hash, size, modified_at, deleted, device_id FROM changes
		WHERE vault_id=? AND path=? AND seq=?`, vaultID, filePath, sourceRevision).
		Scan(&source.Revision, &source.Path, &hash, &source.Size, &source.ModifiedAt, &source.Deleted, &source.DeviceID)
	if err != nil {
		return Change{}, false, err
	}
	source.BlobHash = hash.String
	if source.Deleted {
		hash = sql.NullString{}
		err = s.db.QueryRowContext(ctx, `SELECT seq, path, blob_hash, size, modified_at, deleted, device_id FROM changes
			WHERE vault_id=? AND path=? AND seq<? AND deleted=0 ORDER BY seq DESC LIMIT 1`, vaultID, filePath, sourceRevision).
			Scan(&source.Revision, &source.Path, &hash, &source.Size, &source.ModifiedAt, &source.Deleted, &source.DeviceID)
		if err != nil {
			return Change{}, false, fmt.Errorf("deleted file has no retained content version: %w", err)
		}
		source.BlobHash = hash.String
	}
	if modifiedAt <= 0 {
		modifiedAt = time.Now().UnixMilli()
	}
	var result Change
	var changed bool
	data, loadErr := s.GetBlob(ctx, vaultID, source.BlobHash)
	if loadErr != nil {
		return Change{}, false, fmt.Errorf("load historical content: %w", loadErr)
	}
	result, changed, err = s.PutWithOperation(ctx, vaultID, filePath, deviceID, operationID, baseRevision, modifiedAt, source.BlobHash, data)
	if err != nil || !changed {
		return result, changed, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE changes SET operation_kind='restore', restored_from_revision=? WHERE seq=?`, source.Revision, result.Revision); err != nil {
		return Change{}, false, fmt.Errorf("mark restored revision: %w", err)
	}
	result.OperationKind = "restore"
	result.RestoredFromRevision = source.Revision
	_ = s.RecordAudit(ctx, "history.restored", vaultID, deviceID, "device", "", map[string]any{
		"path_hash": Hash([]byte(filePath)), "source_revision": source.Revision, "revision": result.Revision,
	})
	return result, true, nil
}
