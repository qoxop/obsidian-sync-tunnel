package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const ChunkSize = 4 * 1024 * 1024

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ChunkRef struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func (s *Store) MissingChunks(ctx context.Context, hashes []string) ([]string, error) {
	if len(hashes) > 1000 {
		return nil, errors.New("at most 1000 chunk hashes may be queried")
	}
	missing := make([]string, 0)
	seen := make(map[string]struct{}, len(hashes))
	for _, value := range hashes {
		hash, err := normalizeSHA256(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		var size int64
		err = s.db.QueryRowContext(ctx, `SELECT size FROM chunks WHERE hash=?`, hash).Scan(&size)
		if errors.Is(err, sql.ErrNoRows) {
			missing = append(missing, hash)
			continue
		}
		if err != nil {
			return nil, err
		}
		info, statErr := os.Stat(filepath.Join(s.blobDir, filepath.FromSlash(chunkRelativePath(hash))))
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != size {
			missing = append(missing, hash)
		}
	}
	return missing, nil
}

func (s *Store) PutChunk(ctx context.Context, expectedHash string, data []byte) (bool, error) {
	hash, err := normalizeSHA256(expectedHash)
	if err != nil {
		return false, err
	}
	if len(data) == 0 || len(data) > ChunkSize {
		return false, fmt.Errorf("chunk size must be between 1 and %d bytes", ChunkSize)
	}
	if Hash(data) != hash {
		return false, errors.New("chunk SHA-256 does not match path")
	}
	relative := chunkRelativePath(hash)
	target := filepath.Join(s.blobDir, filepath.FromSlash(relative))
	if existing, readErr := os.ReadFile(target); readErr == nil && Hash(existing) == hash {
		if err := s.recordChunk(ctx, hash, int64(len(existing)), relative); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return false, fmt.Errorf("create chunk directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".sync-tunnel-chunk-*")
	if err != nil {
		return false, fmt.Errorf("create temporary chunk: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return false, err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false, fmt.Errorf("write chunk: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return false, fmt.Errorf("sync chunk: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return false, fmt.Errorf("replace corrupt chunk: %w", removeErr)
		}
		if err := os.Rename(temporaryPath, target); err != nil {
			return false, fmt.Errorf("install chunk: %w", err)
		}
	}
	if err := s.recordChunk(ctx, hash, int64(len(data)), relative); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) GetChunk(ctx context.Context, expectedHash string) ([]byte, error) {
	hash, err := normalizeSHA256(expectedHash)
	if err != nil {
		return nil, err
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chunks WHERE hash=?)`, hash).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, sql.ErrNoRows
	}
	data, err := os.ReadFile(filepath.Join(s.blobDir, filepath.FromSlash(chunkRelativePath(hash))))
	if err != nil {
		return nil, err
	}
	if Hash(data) != hash {
		return nil, errors.New("stored chunk failed SHA-256 verification")
	}
	return data, nil
}

func (s *Store) GetManifest(ctx context.Context, vaultID, contentHash string) (int64, []ChunkRef, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return 0, nil, err
	}
	hash, err := normalizeSHA256(contentHash)
	if err != nil {
		return 0, nil, err
	}
	var referenced bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM changes WHERE vault_id=? AND blob_hash=?)`, vaultID, hash).Scan(&referenced); err != nil {
		return 0, nil, err
	}
	if !referenced {
		return 0, nil, sql.ErrNoRows
	}
	var size int64
	var encoded string
	if err := s.db.QueryRowContext(ctx, `SELECT size, chunks_json FROM manifests WHERE content_hash=?`, hash).Scan(&size, &encoded); err != nil {
		return 0, nil, err
	}
	var chunks []ChunkRef
	if err := json.Unmarshal([]byte(encoded), &chunks); err != nil {
		return 0, nil, fmt.Errorf("decode manifest: %w", err)
	}
	return size, chunks, nil
}

func (s *Store) AssembleChunks(ctx context.Context, contentHash string, expectedSize int64, chunks []ChunkRef) ([]byte, error) {
	hash, err := normalizeSHA256(contentHash)
	if err != nil {
		return nil, fmt.Errorf("invalid content hash: %w", err)
	}
	if expectedSize < 0 {
		return nil, errors.New("file size cannot be negative")
	}
	if len(chunks) == 0 {
		return nil, errors.New("manifest must contain at least one chunk")
	}
	if expectedSize > int64(^uint(0)>>1) {
		return nil, errors.New("file is too large for this server")
	}
	assembled := make([]byte, 0, int(expectedSize))
	for index, chunk := range chunks {
		if chunk.Size < 1 || chunk.Size > ChunkSize {
			return nil, fmt.Errorf("chunk %d has invalid size", index)
		}
		if index < len(chunks)-1 && chunk.Size != ChunkSize {
			return nil, fmt.Errorf("chunk %d must use the fixed chunk size", index)
		}
		data, err := s.GetChunk(ctx, chunk.Hash)
		if err != nil {
			return nil, fmt.Errorf("read chunk %d: %w", index, err)
		}
		if int64(len(data)) != chunk.Size {
			return nil, fmt.Errorf("chunk %d size does not match manifest", index)
		}
		assembled = append(assembled, data...)
		if int64(len(assembled)) > expectedSize {
			return nil, errors.New("manifest chunks exceed file size")
		}
	}
	if int64(len(assembled)) != expectedSize {
		return nil, errors.New("manifest chunks do not match file size")
	}
	if Hash(assembled) != hash {
		return nil, errors.New("manifest content SHA-256 does not match chunks")
	}
	encoded, err := json.Marshal(chunks)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO manifests(content_hash, size, chunks_json, created_at) VALUES(?, ?, ?, ?) ON CONFLICT(content_hash) DO NOTHING`,
		hash, expectedSize, string(encoded), time.Now().UnixMilli()); err != nil {
		return nil, fmt.Errorf("record manifest: %w", err)
	}
	return assembled, nil
}

func (s *Store) recordChunk(ctx context.Context, hash string, size int64, relative string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chunks(hash, size, relative_path, created_at) VALUES(?, ?, ?, ?) ON CONFLICT(hash) DO UPDATE SET size=excluded.size, relative_path=excluded.relative_path`,
		hash, size, filepath.ToSlash(relative), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("record chunk: %w", err)
	}
	return nil
}

func normalizeSHA256(value string) (string, error) {
	value = strings.ToLower(value)
	if !sha256Pattern.MatchString(value) {
		return "", errors.New("invalid SHA-256")
	}
	return value, nil
}

func chunkRelativePath(hash string) string {
	return filepath.ToSlash(filepath.Join("chunks", hash[:2], hash))
}
