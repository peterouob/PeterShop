package postgresql

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/service/seckill-service/internal/domain"
	seckill "github.com/peterouob/seckill_service/service/seckill-service/internal/repository/postgresql/db"
)

type SnapshotRepo struct {
	pool *pgxpool.Pool
}

func NewSnapshotRepo(pool *pgxpool.Pool) *SnapshotRepo {
	return &SnapshotRepo{pool: pool}
}

func (r *SnapshotRepo) LoadSnapshot(ctx context.Context, activityID int64) (domain.ActivitySnapshot, error) {
	q := seckill.New(r.pool)
	row, err := q.LoadActivitySnapshot(ctx, activityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ActivitySnapshot{}, domain.ErrActivityNotFound
		}
		return domain.ActivitySnapshot{}, err
	}

	if row.SlotTotal == 0 {
		return domain.ActivitySnapshot{}, fmt.Errorf("snapshot: activity %d has no slots row", activityID)
	}

	return domain.ActivitySnapshot{
		ActivityID: activityID,
		StartAt:    row.StartAt,
		EndAt:      row.EndAt,
		Sealed:     row.Status >= 3,
		Available:  int64(row.Available),
		Users:      row.UserIds,
	}, nil
}
