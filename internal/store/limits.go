package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type ResourceLimits struct {
	MaxFileBytes      int64
	DefaultQuotaBytes int64
	DefaultMaxFiles   int64
	MinFreeBytes      int64
}

type ResourceLimitError struct {
	Code    string
	Message string
}

func (e *ResourceLimitError) Error() string { return e.Message }

func (s *Store) ConfigureLimits(limits ResourceLimits) {
	s.limits = limits
}

func (s *Store) CheckCommitAllowed(ctx context.Context, vaultID, filePath string, size int64) error {
	if size < 0 {
		return errors.New("file size cannot be negative")
	}
	if s.limits.MaxFileBytes > 0 && size > s.limits.MaxFileBytes {
		return &ResourceLimitError{Code: "file_too_large", Message: fmt.Sprintf("file exceeds %d bytes", s.limits.MaxFileBytes)}
	}
	if err := s.checkDiskSpace(size); err != nil {
		return err
	}
	vault, err := s.GetVault(ctx, vaultID)
	if err != nil {
		return err
	}
	quota := vault.QuotaBytes
	if quota == 0 {
		quota = s.limits.DefaultQuotaBytes
	}
	maxFiles := vault.MaxFiles
	if maxFiles == 0 {
		maxFiles = s.limits.DefaultMaxFiles
	}
	var currentBytes, currentFiles int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0), COUNT(*) FROM files WHERE vault_id=? AND deleted=0`, vaultID).Scan(&currentBytes, &currentFiles); err != nil {
		return err
	}
	var previousSize int64
	var previousDeleted bool
	err = s.db.QueryRowContext(ctx, `SELECT size, deleted FROM files WHERE vault_id=? AND path=?`, vaultID, filePath).Scan(&previousSize, &previousDeleted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	projectedBytes := currentBytes + size
	projectedFiles := currentFiles + 1
	if err == nil && !previousDeleted {
		projectedBytes -= previousSize
		projectedFiles--
	}
	if quota > 0 && projectedBytes > quota {
		return &ResourceLimitError{Code: "vault_quota_exceeded", Message: "vault storage quota would be exceeded"}
	}
	if maxFiles > 0 && projectedFiles > maxFiles {
		return &ResourceLimitError{Code: "vault_file_limit_exceeded", Message: "vault file count limit would be exceeded"}
	}
	return nil
}

func (s *Store) checkDiskSpace(incoming int64) error {
	if s.limits.MinFreeBytes <= 0 {
		return nil
	}
	free, err := freeDiskBytes(s.blobDir)
	if err != nil {
		return fmt.Errorf("check free disk space: %w", err)
	}
	if free-incoming < s.limits.MinFreeBytes {
		return &ResourceLimitError{Code: "insufficient_storage", Message: "server disk free-space safety threshold reached"}
	}
	return nil
}
