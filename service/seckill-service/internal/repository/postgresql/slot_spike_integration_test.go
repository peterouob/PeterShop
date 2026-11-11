//go:build integration

package postgresql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	slotQuota        = 20_000
	slotActivityID   = 1
	slotReplays      = 2_000
	slotFillWorkers  = 64
	slotActiveSlotUK = "uk_ledger_b_active_slot"
)

var slotConcurrency = []int{16, 64, 128}

const slotSchema = `
CREATE SCHEMA spike_slots;

CREATE TABLE spike_slots.slots (
    activity_id bigint not null,
    slot_no     int    not null,
    disabled_at timestamptz(3),
    primary key (activity_id, slot_no)
);

CREATE TABLE spike_slots.ledger_b (
    order_no    bigint primary key,
    activity_id bigint not null,
    user_id     varchar(64) not null,
    slot_no     int not null,
    status      smallint not null,
    locked_at   timestamptz(3) not null,
    created_at  timestamptz(3) not null,
    updated_at  timestamptz(3) not null,
    constraint ck_ledger_b_status check (status in (1, 2, 3)),
    constraint uk_ledger_b_activity_user unique (activity_id, user_id),
    constraint fk_ledger_b_slot foreign key (activity_id, slot_no)
        references spike_slots.slots (activity_id, slot_no) on delete restrict
);

CREATE INDEX idx_ledger_b_activity_status ON spike_slots.ledger_b (activity_id, status);
CREATE UNIQUE INDEX uk_ledger_b_active_slot ON spike_slots.ledger_b (activity_id, slot_no) WHERE status <> 3;

CREATE TABLE spike_slots.outbox_b (LIKE public.outbox INCLUDING ALL);

CREATE FUNCTION spike_slots.ledger_b_check_slot_enabled() RETURNS trigger AS $$
BEGIN
    PERFORM 1 FROM spike_slots.slots
    WHERE activity_id = NEW.activity_id
      AND slot_no = NEW.slot_no
      AND disabled_at IS NULL
    FOR SHARE
    LIMIT 1;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'slot % disabled', NEW.slot_no
            USING errcode = '23514', constraint = 'ck_ledger_b_slot_enabled';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
`

const (
	slotInsertOutbox = `
insert into spike_slots.outbox_b (order_no, payload)
select $1::bigint,
       jsonb_build_object(
               'order_no', $1::bigint,
               'user_id', $3::text,
               'product_id', sa.product_id,
               'activity_id', sa.activity_id,
               'amount', sa.price,
               'created_at_ms', (extract(epoch from now()) * 1000)::bigint)
from seckill_activity sa
where sa.activity_id = $2::bigint`
	slotDeleteOutbox = `delete from spike_slots.outbox_b where order_no = $1`
	// The counter baseline runs against version 8, where stock_lock still exists.
	counterInsertOutbox = `
insert into outbox (order_no, payload)
select $1::bigint,
       jsonb_build_object(
               'order_no', $1::bigint,
               'user_id', $3::text,
               'product_id', sa.product_id,
               'activity_id', sa.activity_id,
               'amount', sa.price,
               'created_at_ms', (extract(epoch from now()) * 1000)::bigint)
from seckill_activity sa
where sa.activity_id = $2::bigint`
	counterDeleteOutbox = `delete from outbox where order_no = $1`
	counterStockLock    = `select stock_lock($1::bigint, $2::bigint, $3::varchar)`
	slotInsertLedger    = `
insert into spike_slots.ledger_b (order_no, activity_id, user_id, slot_no, status, locked_at, created_at, updated_at)
values ($1, $2, $3, $4, 1, now(), now(), now())`
)

type slotSpike struct {
	t     *testing.T
	ctx   context.Context
	dsn   string
	admin *pgxpool.Pool
}

type slotModel struct {
	name    string
	reset   func(s *slotSpike)
	reserve func(ctx context.Context, pool *pgxpool.Pool, i int) error
	active  func(s *slotSpike) int
}

type slotRun struct {
	label    string
	workers  int
	ok       int
	rejected map[string]int
	firstErr error
	lat      []time.Duration
	wall     time.Duration
}

func TestSlotSpike(t *testing.T) {
	if os.Getenv("SPIKE") != "1" {
		t.Skip("set SPIKE=1 to run the inventory model spike")
	}
	s := newSlotSpike(t)
	models := []slotModel{s.counterModel(), s.slotsModel(), s.slotsTriggerModel()}

	var runs []slotRun
	for _, m := range models {
		for _, c := range slotConcurrency {
			t.Run(fmt.Sprintf("%s/c%d", m.name, c), func(t *testing.T) {
				s.t = t
				m.reset(s)
				r := drive(t.Context(), s.pool(c), c, slotQuota, m.reserve)
				r.label = m.name
				require.Empty(t, r.rejected, "first error: %v", r.firstErr)
				active := m.active(s)
				require.Equal(t, r.ok, active, "active rows must equal successes")
				require.LessOrEqual(t, active, slotQuota)
				runs = append(runs, r)
			})
		}
	}

	t.Run("slots/collision", func(t *testing.T) {
		s.t = t
		runs = append(runs, s.collision(t)...)
	})
	s.t = t

	logSlotTable(t, runs)
}

func newSlotSpike(t *testing.T) *slotSpike {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("db_seckill"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithCmd("postgres", "-c", "max_connections=300", "-c", "wal_level=logical"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pg)
	require.NoError(t, err)

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	admin, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	s := &slotSpike{t: t, ctx: ctx, dsn: dsn, admin: admin}
	s.exec(`CREATE ROLE svc_cdc NOLOGIN`)
	s.migrate()
	s.exec(slotSchema)
	return s
}

func (s *slotSpike) migrate() {
	dir := filepath.Join("..", "..", "..", "..", "..", "deploy", "migrations", "seckill")
	m, err := migrate.New("file://"+filepath.ToSlash(dir), strings.Replace(s.dsn, "postgres://", "pgx5://", 1))
	require.NoError(s.t, err)
	defer func() { _, _ = m.Close() }()
	// The counter baseline needs activity_stock, which version 9 drops.
	require.NoError(s.t, m.Migrate(8))
}

func (s *slotSpike) exec(sql string, args ...any) {
	s.t.Helper()
	_, err := s.admin.Exec(s.ctx, sql, args...)
	require.NoError(s.t, err, sql)
}

func (s *slotSpike) count(sql string) int {
	s.t.Helper()
	var n int
	require.NoError(s.t, s.admin.QueryRow(s.ctx, sql).Scan(&n), sql)
	return n
}

// pool is sized to at least the worker count and pre-warmed so connect time stays out of the latencies.
func (s *slotSpike) pool(workers int) *pgxpool.Pool {
	s.t.Helper()
	cfg, err := pgxpool.ParseConfig(s.dsn)
	require.NoError(s.t, err)
	maxConns := workers
	if v := os.Getenv("TEST_PG_MAXCONNS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoError(s.t, err, "TEST_PG_MAXCONNS must be an integer")
		maxConns = max(maxConns, n)
	}
	cfg.MaxConns = int32(maxConns)
	pool, err := pgxpool.NewWithConfig(s.ctx, cfg)
	require.NoError(s.t, err)
	s.t.Cleanup(pool.Close)

	conns := make([]*pgxpool.Conn, 0, workers)
	for range workers {
		c, err := pool.Acquire(s.ctx)
		require.NoError(s.t, err)
		conns = append(conns, c)
	}
	for _, c := range conns {
		c.Release()
	}
	return pool
}

func (s *slotSpike) seedActivity() {
	start := time.Now().Add(-time.Minute)
	s.exec(`TRUNCATE stock_ledger, outbox, activity_stock, seckill_activity,
	        spike_slots.ledger_b, spike_slots.outbox_b, spike_slots.slots`)
	s.exec(`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
	        VALUES ($1, 'p-spike', 100, $2, $3, 2, now(), now())`,
		slotActivityID, start, start.Add(time.Hour))
	s.exec(`INSERT INTO activity_stock (activity_id, total, available, locked, sold) VALUES ($1, $2, $2, 0, 0)`,
		slotActivityID, slotQuota)
}

func (s *slotSpike) settle() {
	s.exec(`VACUUM ANALYZE`)
	s.exec(`CHECKPOINT`)
}

func (s *slotSpike) counterModel() slotModel {
	return slotModel{
		name: "counter",
		reset: func(s *slotSpike) {
			s.seedActivity()
			s.settle()
		},
		reserve: func(ctx context.Context, pool *pgxpool.Pool, i int) error {
			return insertCounter(ctx, pool, int64(i+1), slotUser("u", i))
		},
		active: func(s *slotSpike) int {
			ledger := s.count(`SELECT count(*) FROM stock_ledger WHERE status = 1`)
			locked := s.count(`SELECT locked FROM activity_stock`)
			require.Equal(s.t, ledger, locked, "activity_stock.locked must match ledger")
			return ledger
		},
	}
}

func (s *slotSpike) slotsModel() slotModel {
	return slotModel{
		name:  "slots",
		reset: (*slotSpike).resetSlots,
		reserve: func(ctx context.Context, pool *pgxpool.Pool, i int) error {
			return insertSlot(ctx, pool, int64(i+1), slotUser("u", i), i+1)
		},
		active: (*slotSpike).activeSlots,
	}
}

func (s *slotSpike) slotsTriggerModel() slotModel {
	m := s.slotsModel()
	m.name = "slots_trigger"
	m.reset = func(s *slotSpike) {
		s.resetSlots()
		s.exec(`CREATE TRIGGER trg_ledger_b_slot_enabled BEFORE INSERT ON spike_slots.ledger_b
		        FOR EACH ROW WHEN (NEW.status <> 3) EXECUTE FUNCTION spike_slots.ledger_b_check_slot_enabled()`)
	}
	return m
}

func (s *slotSpike) resetSlots() {
	s.exec(`DROP TRIGGER IF EXISTS trg_ledger_b_slot_enabled ON spike_slots.ledger_b`)
	s.seedActivity()
	s.exec(`INSERT INTO spike_slots.slots (activity_id, slot_no) SELECT $1, g FROM generate_series(1, $2::int) g`,
		slotActivityID, slotQuota)
	s.settle()
}

func (s *slotSpike) activeSlots() int {
	return s.count(`SELECT count(*) FROM spike_slots.ledger_b WHERE status <> 3`)
}

func (s *slotSpike) collision(t *testing.T) []slotRun {
	s.resetSlots()
	pool := s.pool(slotFillWorkers)
	half := slotQuota / 2

	fill := drive(t.Context(), pool, slotFillWorkers, half, func(ctx context.Context, pool *pgxpool.Pool, i int) error {
		return insertSlot(ctx, pool, int64(i+1), slotUser("u", i), i+1)
	})
	fill.label = "slots/fill-half"
	require.Empty(t, fill.rejected, "first error: %v", fill.firstErr)

	replay := drive(t.Context(), pool, slotFillWorkers, slotReplays, func(ctx context.Context, pool *pgxpool.Pool, i int) error {
		return insertSlot(ctx, pool, int64(slotQuota+i+1), slotUser("r", i), i%half+1)
	})
	replay.label = "slots/reissued"

	t.Logf("reissued slots: ok=%d rejected=%v", replay.ok, replay.rejected)
	require.Zero(t, replay.ok, "a reissued slot must never be accepted")
	require.Equal(t, slotReplays, replay.rejected["23505/"+slotActiveSlotUK])

	active := s.activeSlots()
	require.Equal(t, fill.ok, active)
	require.LessOrEqual(t, active, slotQuota)
	return []slotRun{fill, replay}
}

func insertSlot(ctx context.Context, pool *pgxpool.Pool, orderNo int64, userID string, slotNo int) error {
	return pgx.BeginTxFunc(ctx, pool, txOpts, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, slotInsertOutbox, orderNo, slotActivityID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, slotDeleteOutbox, orderNo); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, slotInsertLedger, orderNo, slotActivityID, userID, slotNo)
		return err
	})
}

func insertCounter(ctx context.Context, pool *pgxpool.Pool, orderNo int64, userID string) error {
	return pgx.BeginTxFunc(ctx, pool, txOpts, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, counterInsertOutbox, orderNo, slotActivityID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, counterDeleteOutbox, orderNo); err != nil {
			return err
		}
		var locked bool
		if err := tx.QueryRow(ctx, counterStockLock, orderNo, slotActivityID, userID).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return fmt.Errorf("%w: order_no=%d", ErrDuplicateOrderNo, orderNo)
		}
		return nil
	})
}

func slotUser(prefix string, i int) string {
	return prefix + strconv.Itoa(i)
}

func drive(ctx context.Context, pool *pgxpool.Pool, workers, n int, fn func(context.Context, *pgxpool.Pool, int) error) slotRun {
	r := slotRun{workers: workers, rejected: map[string]int{}}
	var (
		next atomic.Int64
		mu   sync.Mutex
		wg   sync.WaitGroup
	)
	gate := make(chan struct{})

	for range workers {
		wg.Go(func() {
			<-gate
			var (
				ok   int
				lat  []time.Duration
				errs []error
			)
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				start := time.Now()
				err := fn(ctx, pool, i)
				lat = append(lat, time.Since(start))
				if err != nil {
					errs = append(errs, err)
					continue
				}
				ok++
			}
			mu.Lock()
			defer mu.Unlock()
			r.ok += ok
			r.lat = append(r.lat, lat...)
			for _, err := range errs {
				r.rejected[errKey(err)]++
				if r.firstErr == nil {
					r.firstErr = err
				}
			}
		})
	}
	start := time.Now()
	close(gate)
	wg.Wait()
	r.wall = time.Since(start)
	return r
}

func errKey(err error) string {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code + "/" + pgErr.ConstraintName
	}
	return "other"
}

func logSlotTable(t *testing.T, runs []slotRun) {
	t.Helper()
	t.Logf("%-16s %5s %7s %8s %9s %9s %9s %9s", "model", "conc", "ok", "rejected", "wall", "tx/s", "p50", "p99")
	for _, r := range runs {
		s := slices.Clone(r.lat)
		slices.Sort(s)
		at := func(p float64) time.Duration {
			if len(s) == 0 {
				return 0
			}
			return s[int(p*float64(len(s)-1))]
		}
		rejected := 0
		for _, v := range r.rejected {
			rejected += v
		}
		t.Logf("%-16s %5d %7d %8d %9s %9.0f %9s %9s", r.label, r.workers, r.ok, rejected,
			r.wall.Round(time.Millisecond), float64(len(r.lat))/r.wall.Seconds(),
			at(0.50).Round(10*time.Microsecond), at(0.99).Round(10*time.Microsecond))
	}
}
