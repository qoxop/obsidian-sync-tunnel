package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (s *Store) RenameWithOperation(
	ctx context.Context,
	vaultID, fromPath, toPath, deviceID, operationID string,
	baseRevision, modifiedAt int64,
) (Change, []Change, bool, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return Change{}, nil, false, err
	}
	if err := ValidateID("device ID", deviceID); err != nil {
		return Change{}, nil, false, err
	}
	if !operationIDPattern.MatchString(operationID) {
		return Change{}, nil, false, errors.New("invalid operation ID")
	}
	var err error
	fromPath, err = NormalizePath(fromPath)
	if err != nil {
		return Change{}, nil, false, fmt.Errorf("invalid source path: %w", err)
	}
	toPath, err = NormalizePath(toPath)
	if err != nil {
		return Change{}, nil, false, fmt.Errorf("invalid destination path: %w", err)
	}
	if fromPath == toPath {
		return Change{}, nil, false, errors.New("rename paths must be different")
	}
	if baseRevision < 0 {
		return Change{}, nil, false, errors.New("base revision cannot be negative")
	}
	if modifiedAt <= 0 {
		modifiedAt = time.Now().UnixMilli()
	}
	fingerprint := Hash([]byte(fmt.Sprintf("rename\n%s\n%s\n%d\n%d", fromPath, toPath, baseRevision, modifiedAt)))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Change{}, nil, false, err
	}
	defer tx.Rollback()
	if result, related, changed, storedFingerprint, found, err := queryOperationDetails(ctx, tx, vaultID, deviceID, operationID); err != nil {
		return Change{}, nil, false, err
	} else if found {
		if storedFingerprint != fingerprint {
			return Change{}, nil, false, &OperationReuseError{OperationID: operationID}
		}
		return result, related, changed, nil
	}
	source, found, err := currentFile(ctx, tx, vaultID, fromPath)
	if err != nil {
		return Change{}, nil, false, err
	}
	if !found || source.Deleted || source.Revision != baseRevision {
		if !found {
			source = Change{Path: fromPath, Deleted: true}
		}
		return Change{}, nil, false, &ConflictError{Current: source}
	}
	destination, destinationFound, err := currentFile(ctx, tx, vaultID, toPath)
	if err != nil {
		return Change{}, nil, false, err
	}
	if destinationFound && !destination.Deleted {
		return Change{}, nil, false, &ConflictError{Current: destination}
	}
	now := time.Now().UnixMilli()
	oldResult, err := tx.ExecContext(ctx,
		`INSERT INTO changes(vault_id, path, blob_hash, size, modified_at, deleted, device_id, created_at) VALUES(?, ?, NULL, 0, ?, 1, ?, ?)`,
		vaultID, fromPath, modifiedAt, deviceID, now,
	)
	if err != nil {
		return Change{}, nil, false, fmt.Errorf("append rename tombstone: %w", err)
	}
	oldRevision, err := oldResult.LastInsertId()
	if err != nil {
		return Change{}, nil, false, err
	}
	newResult, err := tx.ExecContext(ctx,
		`INSERT INTO changes(vault_id, path, blob_hash, size, modified_at, deleted, device_id, created_at) VALUES(?, ?, ?, ?, ?, 0, ?, ?)`,
		vaultID, toPath, source.BlobHash, source.Size, modifiedAt, deviceID, now,
	)
	if err != nil {
		return Change{}, nil, false, fmt.Errorf("append rename destination: %w", err)
	}
	newRevision, err := newResult.LastInsertId()
	if err != nil {
		return Change{}, nil, false, err
	}
	oldChange := Change{Revision: oldRevision, Path: fromPath, Size: 0, ModifiedAt: modifiedAt, Deleted: true, DeviceID: deviceID}
	newChange := Change{Revision: newRevision, Path: toPath, BlobHash: source.BlobHash, Size: source.Size, ModifiedAt: modifiedAt, DeviceID: deviceID}
	if err := upsertRenamedFile(ctx, tx, vaultID, oldChange, now); err != nil {
		return Change{}, nil, false, err
	}
	if err := upsertRenamedFile(ctx, tx, vaultID, newChange, now); err != nil {
		return Change{}, nil, false, err
	}
	if err := recordOperationDetails(ctx, tx, vaultID, deviceID, operationID, fingerprint, newChange, []Change{oldChange}, true); err != nil {
		return Change{}, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return Change{}, nil, false, err
	}
	return newChange, []Change{oldChange}, true, nil
}

func queryOperationDetails(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, vaultID, deviceID, operationID string) (Change, []Change, bool, string, bool, error) {
	var fingerprint, encoded, relatedEncoded string
	var changed bool
	err := q.QueryRowContext(ctx, `SELECT fingerprint, change_json, related_json, changed FROM operations WHERE vault_id=? AND device_id=? AND operation_id=?`, vaultID, deviceID, operationID).
		Scan(&fingerprint, &encoded, &relatedEncoded, &changed)
	if errors.Is(err, sql.ErrNoRows) {
		return Change{}, nil, false, "", false, nil
	}
	if err != nil {
		return Change{}, nil, false, "", false, err
	}
	var change Change
	var related []Change
	if err := json.Unmarshal([]byte(encoded), &change); err != nil {
		return Change{}, nil, false, "", false, err
	}
	if err := json.Unmarshal([]byte(relatedEncoded), &related); err != nil {
		return Change{}, nil, false, "", false, err
	}
	return change, related, changed, fingerprint, true, nil
}

func recordOperationDetails(ctx context.Context, tx *sql.Tx, vaultID, deviceID, operationID, fingerprint string, change Change, related []Change, changed bool) error {
	encoded, err := json.Marshal(change)
	if err != nil {
		return err
	}
	relatedEncoded, err := json.Marshal(related)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operations(vault_id, device_id, operation_id, fingerprint, change_json, related_json, changed, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		vaultID, deviceID, operationID, fingerprint, string(encoded), string(relatedEncoded), changed, time.Now().UnixMilli())
	return err
}

func upsertRenamedFile(ctx context.Context, tx *sql.Tx, vaultID string, change Change, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO files(vault_id, path, revision, blob_hash, size, modified_at, deleted, device_id, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vault_id, path) DO UPDATE SET revision=excluded.revision, blob_hash=excluded.blob_hash,
		size=excluded.size, modified_at=excluded.modified_at, deleted=excluded.deleted, device_id=excluded.device_id, updated_at=excluded.updated_at`,
		vaultID, change.Path, change.Revision, nullableHash(change.BlobHash), change.Size, change.ModifiedAt, change.Deleted, change.DeviceID, now)
	return err
}
