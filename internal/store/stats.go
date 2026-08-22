package store

import "context"

type Stats struct {
	Vaults        int64 `json:"vaults"`
	ActiveDevices int64 `json:"active_devices"`
	CurrentFiles  int64 `json:"current_files"`
	LogicalBytes  int64 `json:"logical_bytes"`
	Revisions     int64 `json:"revisions"`
	Chunks        int64 `json:"chunks"`
	ChunkBytes    int64 `json:"chunk_bytes"`
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var result Stats
	queries := []struct {
		query   string
		targets []any
	}{
		{`SELECT COUNT(*) FROM vaults`, []any{&result.Vaults}},
		{`SELECT COUNT(*) FROM devices WHERE status='active'`, []any{&result.ActiveDevices}},
		{`SELECT COUNT(*), COALESCE(SUM(size),0) FROM files WHERE deleted=0`, []any{&result.CurrentFiles, &result.LogicalBytes}},
		{`SELECT COUNT(*) FROM changes`, []any{&result.Revisions}},
		{`SELECT COUNT(*), COALESCE(SUM(size),0) FROM chunks`, []any{&result.Chunks, &result.ChunkBytes}},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(ctx, item.query).Scan(item.targets...); err != nil {
			return Stats{}, err
		}
	}
	return result, nil
}
