package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type BatchDeleteItem struct {
	Path         string `json:"path"`
	BaseRevision int64  `json:"base_revision"`
	ModifiedAt   int64  `json:"modified_at"`
}

func (s *Store) BatchDeleteWithOperation(
	ctx context.Context,
	vaultID, deviceID, operationID string,
	items []BatchDeleteItem,
) ([]Change, bool, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return nil, false, err
	}
	if err := ValidateID("device ID", deviceID); err != nil {
		return nil, false, err
	}
	if !operationIDPattern.MatchString(operationID) {
		return nil, false, errors.New("invalid operation ID")
	}
	if len(items) < 1 || len(items) > 100 {
		return nil, false, errors.New("batch delete must contain between 1 and 100 items")
	}
	normalized := make([]BatchDeleteItem, len(items))
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		path, err := NormalizePath(item.Path)
		if err != nil {
			return nil, false, fmt.Errorf("invalid batch path: %w", err)
		}
		if _, exists := seen[path]; exists {
			return nil, false, fmt.Errorf("duplicate batch path %q", path)
		}
		seen[path] = struct{}{}
		if item.BaseRevision < 0 {
			return nil, false, errors.New("base revision cannot be negative")
		}
		if item.ModifiedAt <= 0 {
			item.ModifiedAt = time.Now().UnixMilli()
		}
		item.Path = path
		normalized[index] = item
	}
	sort.Slice(normalized, func(left, right int) bool { return normalized[left].Path < normalized[right].Path })
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, false, err
	}
	fingerprint := Hash(append([]byte("batch-delete\n"), encoded...))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if result, related, changed, storedFingerprint, found, err := queryOperationDetails(ctx, tx, vaultID, deviceID, operationID); err != nil {
		return nil, false, err
	} else if found {
		if storedFingerprint != fingerprint {
			return nil, false, &OperationReuseError{OperationID: operationID}
		}
		return append([]Change{result}, related...), changed, nil
	}
	for _, item := range normalized {
		current, found, err := currentFile(ctx, tx, vaultID, item.Path)
		if err != nil {
			return nil, false, err
		}
		if !found || current.Deleted || current.Revision != item.BaseRevision {
			if !found {
				current = Change{Path: item.Path, Deleted: true}
			}
			return nil, false, &ConflictError{Current: current}
		}
	}
	now := time.Now().UnixMilli()
	changes := make([]Change, 0, len(normalized))
	for _, item := range normalized {
		result, err := tx.ExecContext(ctx,
			`INSERT INTO changes(vault_id, path, blob_hash, size, modified_at, deleted, device_id, created_at) VALUES(?, ?, NULL, 0, ?, 1, ?, ?)`,
			vaultID, item.Path, item.ModifiedAt, deviceID, now,
		)
		if err != nil {
			return nil, false, fmt.Errorf("append batch tombstone: %w", err)
		}
		revision, err := result.LastInsertId()
		if err != nil {
			return nil, false, err
		}
		change := Change{Revision: revision, Path: item.Path, Size: 0, ModifiedAt: item.ModifiedAt, Deleted: true, DeviceID: deviceID}
		if err := upsertRenamedFile(ctx, tx, vaultID, change, now); err != nil {
			return nil, false, err
		}
		changes = append(changes, change)
	}
	if err := recordOperationDetails(ctx, tx, vaultID, deviceID, operationID, fingerprint, changes[0], changes[1:], true); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return changes, true, nil
}
