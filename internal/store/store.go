package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

const CurrentSchemaVersion = 1

var migrations = [][]string{
	{
		`CREATE TABLE IF NOT EXISTS blobs (
			hash TEXT PRIMARY KEY,
			size INTEGER NOT NULL,
			data BLOB NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			vault_id TEXT NOT NULL,
			path TEXT NOT NULL,
			revision INTEGER NOT NULL,
			blob_hash TEXT,
			size INTEGER NOT NULL,
			modified_at INTEGER NOT NULL,
			deleted INTEGER NOT NULL,
			device_id TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (vault_id, path),
			FOREIGN KEY (blob_hash) REFERENCES blobs(hash)
		)`,
		`CREATE TABLE IF NOT EXISTS changes (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			vault_id TEXT NOT NULL,
			path TEXT NOT NULL,
			blob_hash TEXT,
			size INTEGER NOT NULL,
			modified_at INTEGER NOT NULL,
			deleted INTEGER NOT NULL,
			device_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (blob_hash) REFERENCES blobs(hash)
		)`,
		`CREATE INDEX IF NOT EXISTS changes_vault_seq ON changes(vault_id, seq)`,
		`CREATE INDEX IF NOT EXISTS changes_vault_blob ON changes(vault_id, blob_hash)`,
		`CREATE INDEX IF NOT EXISTS changes_vault_path_seq ON changes(vault_id, path, seq DESC)`,
	},
}

type Store struct {
	db *sql.DB
}

type Change struct {
	Revision   int64  `json:"revision"`
	Path       string `json:"path"`
	BlobHash   string `json:"blob_hash,omitempty"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modified_at"`
	Deleted    bool   `json:"deleted"`
	DeviceID   string `json:"device_id"`
}

type ConflictError struct {
	Current Change
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("revision conflict: current revision is %d", e.Current.Revision)
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initialize(ctx context.Context) error {
	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	}
	for _, statement := range pragmas {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current > CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, CurrentSchemaVersion)
	}
	for version := current + 1; version <= CurrentSchemaVersion; version++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin schema migration %d: %w", version, err)
		}
		for _, statement := range migrations[version-1] {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply schema migration %d: %w", version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record schema migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema migration %d: %w", version, err)
		}
	}
	return nil
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read database schema version: %w", err)
	}
	return version, nil
}

func ValidateID(kind, value string) error {
	if !idPattern.MatchString(value) {
		return fmt.Errorf("invalid %s", kind)
	}
	return nil
}

func NormalizePath(value string) (string, error) {
	value = strings.ReplaceAll(value, `\`, "/")
	value = strings.TrimPrefix(value, "./")
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\x00') {
		return "", errors.New("invalid path")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid path")
		}
	}
	if len(value) > 4096 {
		return "", errors.New("path is too long")
	}
	return value, nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Store) Put(ctx context.Context, vaultID, filePath, deviceID string, baseRevision, modifiedAt int64, expectedHash string, data []byte) (Change, bool, error) {
	actualHash := Hash(data)
	if actualHash != strings.ToLower(expectedHash) {
		return Change{}, false, errors.New("content SHA-256 does not match header")
	}
	return s.apply(ctx, vaultID, filePath, deviceID, baseRevision, modifiedAt, actualHash, data, false)
}

func (s *Store) Delete(ctx context.Context, vaultID, filePath, deviceID string, baseRevision, modifiedAt int64) (Change, bool, error) {
	return s.apply(ctx, vaultID, filePath, deviceID, baseRevision, modifiedAt, "", nil, true)
}

func (s *Store) apply(ctx context.Context, vaultID, filePath, deviceID string, baseRevision, modifiedAt int64, hash string, data []byte, deleted bool) (Change, bool, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return Change{}, false, err
	}
	if err := ValidateID("device ID", deviceID); err != nil {
		return Change{}, false, err
	}
	var err error
	filePath, err = NormalizePath(filePath)
	if err != nil {
		return Change{}, false, err
	}
	if baseRevision < 0 {
		return Change{}, false, errors.New("base revision cannot be negative")
	}
	if modifiedAt <= 0 {
		modifiedAt = time.Now().UnixMilli()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Change{}, false, err
	}
	defer tx.Rollback()

	current, found, err := currentFile(ctx, tx, vaultID, filePath)
	if err != nil {
		return Change{}, false, err
	}
	if found && current.Revision != baseRevision {
		if (deleted && current.Deleted) || (!deleted && !current.Deleted && current.BlobHash == hash) {
			return current, false, tx.Commit()
		}
		return Change{}, false, &ConflictError{Current: current}
	}
	if !found && baseRevision != 0 {
		return Change{}, false, &ConflictError{Current: Change{Path: filePath, Deleted: true}}
	}
	if !found && deleted {
		return Change{Path: filePath, Deleted: true, DeviceID: deviceID}, false, tx.Commit()
	}
	if found && current.Revision == baseRevision && current.Deleted == deleted && (deleted || current.BlobHash == hash) {
		return current, false, tx.Commit()
	}

	now := time.Now().UnixMilli()
	if !deleted {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO blobs(hash, size, data, created_at) VALUES(?, ?, ?, ?) ON CONFLICT(hash) DO NOTHING`,
			hash, len(data), data, now,
		); err != nil {
			return Change{}, false, fmt.Errorf("store blob: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO changes(vault_id, path, blob_hash, size, modified_at, deleted, device_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		vaultID, filePath, nullableHash(hash), len(data), modifiedAt, deleted, deviceID, now,
	)
	if err != nil {
		return Change{}, false, fmt.Errorf("append change: %w", err)
	}
	revision, err := result.LastInsertId()
	if err != nil {
		return Change{}, false, fmt.Errorf("read revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO files(vault_id, path, revision, blob_hash, size, modified_at, deleted, device_id, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vault_id, path) DO UPDATE SET revision=excluded.revision, blob_hash=excluded.blob_hash,
		size=excluded.size, modified_at=excluded.modified_at, deleted=excluded.deleted, device_id=excluded.device_id, updated_at=excluded.updated_at`,
		vaultID, filePath, revision, nullableHash(hash), len(data), modifiedAt, deleted, deviceID, now,
	); err != nil {
		return Change{}, false, fmt.Errorf("update file: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Change{}, false, err
	}
	return Change{Revision: revision, Path: filePath, BlobHash: hash, Size: int64(len(data)), ModifiedAt: modifiedAt, Deleted: deleted, DeviceID: deviceID}, true, nil
}

func currentFile(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, vaultID, filePath string) (Change, bool, error) {
	var change Change
	var hash sql.NullString
	err := q.QueryRowContext(ctx, `SELECT revision, path, blob_hash, size, modified_at, deleted, device_id FROM files WHERE vault_id=? AND path=?`, vaultID, filePath).
		Scan(&change.Revision, &change.Path, &hash, &change.Size, &change.ModifiedAt, &change.Deleted, &change.DeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return Change{}, false, nil
	}
	if err != nil {
		return Change{}, false, err
	}
	change.BlobHash = hash.String
	return change, true, nil
}

func (s *Store) ListChanges(ctx context.Context, vaultID string, after int64, limit int) ([]Change, bool, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return nil, false, err
	}
	if after < 0 {
		return nil, false, errors.New("cursor cannot be negative")
	}
	if limit < 1 || limit > 1000 {
		return nil, false, errors.New("limit must be between 1 and 1000")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, path, blob_hash, size, modified_at, deleted, device_id
		FROM changes WHERE vault_id=? AND seq>? ORDER BY seq ASC LIMIT ?`, vaultID, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	changes := make([]Change, 0, limit)
	for rows.Next() {
		var change Change
		var hash sql.NullString
		if err := rows.Scan(&change.Revision, &change.Path, &hash, &change.Size, &change.ModifiedAt, &change.Deleted, &change.DeviceID); err != nil {
			return nil, false, err
		}
		change.BlobHash = hash.String
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(changes) > limit
	if hasMore {
		changes = changes[:limit]
	}
	return changes, hasMore, nil
}

// ListSnapshot returns the last change for every path at or before atRevision.
// Paging is stable while retained changes remain available because every page
// is evaluated against the same revision instead of the mutable files table.
func (s *Store) ListSnapshot(ctx context.Context, vaultID string, atRevision int64, afterPath string, limit int) ([]Change, bool, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return nil, false, err
	}
	if atRevision < 0 {
		return nil, false, errors.New("snapshot revision cannot be negative")
	}
	if limit < 1 || limit > 1000 {
		return nil, false, errors.New("limit must be between 1 and 1000")
	}
	if afterPath != "" {
		var err error
		afterPath, err = NormalizePath(afterPath)
		if err != nil {
			return nil, false, fmt.Errorf("invalid snapshot cursor: %w", err)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.seq, c.path, c.blob_hash, c.size, c.modified_at, c.deleted, c.device_id
		FROM changes c
		JOIN (
			SELECT path, MAX(seq) AS seq
			FROM changes
			WHERE vault_id=? AND seq<=?
			GROUP BY path
		) latest ON latest.path=c.path AND latest.seq=c.seq
		WHERE c.vault_id=? AND c.path>?
		ORDER BY c.path ASC
		LIMIT ?`, vaultID, atRevision, vaultID, afterPath, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	changes := make([]Change, 0, limit)
	for rows.Next() {
		var change Change
		var hash sql.NullString
		if err := rows.Scan(&change.Revision, &change.Path, &hash, &change.Size, &change.ModifiedAt, &change.Deleted, &change.DeviceID); err != nil {
			return nil, false, err
		}
		change.BlobHash = hash.String
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(changes) > limit
	if hasMore {
		changes = changes[:limit]
	}
	return changes, hasMore, nil
}

func (s *Store) GetBlob(ctx context.Context, vaultID, hash string) ([]byte, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return nil, err
	}
	if len(hash) != 64 {
		return nil, sql.ErrNoRows
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM changes WHERE vault_id=? AND blob_hash=?)`, vaultID, strings.ToLower(hash)).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, sql.ErrNoRows
	}
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT data FROM blobs WHERE hash=?`, strings.ToLower(hash)).Scan(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Store) LatestRevision(ctx context.Context, vaultID string) (int64, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return 0, err
	}
	var revision sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM changes WHERE vault_id=?`, vaultID).Scan(&revision); err != nil {
		return 0, err
	}
	return revision.Int64, nil
}

func nullableHash(hash string) any {
	if hash == "" {
		return nil
	}
	return hash
}
