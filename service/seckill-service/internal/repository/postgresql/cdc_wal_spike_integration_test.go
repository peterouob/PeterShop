//go:build integration && cdc

package postgresql

import (
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	walSpikeOrders  = 5000
	walSpikeWorkers = 32
	walSpikeStock   = 6000
	walNoiseBytes   = 64 << 20
	walCaughtUp     = 1 << 20
	walProdRate     = 2500
)

func skipUnlessWALSpike(t *testing.T) {
	if os.Getenv("SPIKE") != "1" {
		t.Skip("set SPIKE=1 to run the WAL retention spike")
	}
}

type walBatch struct {
	bytes int64
	fpi   int64
	ckpt  bool
	lat   []time.Duration
	wall  time.Duration
}

func (w walBatch) pct(p float64) time.Duration {
	return w.lat[int(p*float64(len(w.lat)-1))].Round(10 * time.Microsecond)
}

func TestWALSpike_BytesPerReserve(t *testing.T) {
	skipUnlessWALSpike(t)
	b := newCDCBench(t)
	pool := b.reservePool(walSpikeWorkers)
	b.seedActivity(1, "p1", 100, walSpikeStock)
	b.seedActivity(2, "p1", 100, walSpikeStock)

	up := b.reserveBatch(pool, 1, 1)
	require.Len(t, countByKey(b.consume(walSpikeOrders, 3*time.Minute)), walSpikeOrders)
	b.waitCaughtUp(2 * time.Minute)

	b.stopDebezium()
	down := b.reserveBatch(pool, 2, walSpikeOrders+1)
	require.Positive(t, down.bytes)

	start := time.Now()
	b.restartDebezium()
	ready := time.Since(start)
	counts := countByKey(b.consume(2*walSpikeOrders, 5*time.Minute))
	delivered := time.Since(start)
	downKeys := 0
	for i := walSpikeOrders + 1; i <= 2*walSpikeOrders; i++ {
		if counts[strconv.Itoa(i)] > 0 {
			downKeys++
		}
	}
	require.Equal(t, walSpikeOrders, downKeys)
	drained := b.waitCaughtUp(2*time.Minute) + delivered

	perReserve := float64(down.bytes) / walSpikeOrders
	toFill := float64(1<<30) / perReserve
	t.Logf("%-14s %10s %8s %6s %9s %9s %9s %9s", "batch", "wal_bytes", "fpi", "ckpt", "B/reserve", "wall", "p50", "p99")
	for _, r := range []struct {
		name string
		w    walBatch
	}{{"debezium up", up}, {"debezium down", down}} {
		t.Logf("%-14s %10d %8d %6v %9.0f %9s %9s %9s", r.name, r.w.bytes, r.w.fpi, r.w.ckpt,
			float64(r.w.bytes)/walSpikeOrders, r.w.wall.Round(time.Millisecond), r.w.pct(0.50), r.w.pct(0.99))
	}
	t.Logf("orders per 1GB retained WAL: %.0f", toFill)
	t.Logf("minutes to fill 1GB at %d orders/s: %.1f", walProdRate, toFill/walProdRate/60)
	t.Logf("catch-up: ready %s, all %d delivered %s, slot lag < 1MB %s",
		ready.Round(time.Millisecond), walSpikeOrders, delivered.Round(time.Millisecond), drained.Round(time.Millisecond))
}

func TestWALSpike_HeartbeatAdvancesSlotWithoutSeckillWrites(t *testing.T) {
	skipUnlessWALSpike(t)
	b := newCDCBench(t)
	b.waitCaughtUp(time.Minute)
	b.assertHeartbeatAdvancesSlot()
}

func TestWALSpike_HeartbeatAfterRestart(t *testing.T) {
	skipUnlessWALSpike(t)
	b := newCDCBench(t)
	b.waitCaughtUp(time.Minute)
	b.stopDebezium()
	b.restartDebezium()
	b.assertHeartbeatAdvancesSlot()
}

func (b *cdcBench) reservePool(conns int) *pgxpool.Pool {
	b.t.Helper()
	cfg, err := pgxpool.ParseConfig(b.dsn("postgres", "svc_seckill", cdcPassword, "db_seckill"))
	require.NoError(b.t, err)
	cfg.MaxConns = int32(conns)
	cfg.MinConns = int32(conns)
	pool, err := pgxpool.NewWithConfig(b.ctx, cfg)
	require.NoError(b.t, err)
	b.t.Cleanup(pool.Close)
	require.Eventually(b.t, func() bool { return pool.Stat().TotalConns() == int32(conns) }, 10*time.Second, 50*time.Millisecond)
	return pool
}

// reserveBatch runs walSpikeOrders real Reserve calls and records the WAL they produced.
func (b *cdcBench) reserveBatch(pool *pgxpool.Pool, activityID int64, firstOrder int) walBatch {
	b.t.Helper()
	repo := &ReserveRepo{pool: pool}
	lagBefore, fpiBefore, redoBefore := b.slotLag(), b.walFPI(), b.redoLSN()

	lat := make([]time.Duration, walSpikeOrders)
	errs := make([]error, walSpikeOrders)
	var next atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for range walSpikeWorkers {
		wg.Go(func() {
			for i := int(next.Add(1) - 1); i < walSpikeOrders; i = int(next.Add(1) - 1) {
				orderNo := firstOrder + i
				t0 := time.Now()
				errs[i] = repo.Reserve(b.ctx, int64(orderNo), activityID, "u"+strconv.Itoa(orderNo))
				lat[i] = time.Since(t0)
			}
		})
	}
	wg.Wait()
	wall := time.Since(start)
	require.NoError(b.t, errors.Join(errs...))
	slices.Sort(lat)

	return walBatch{
		bytes: b.slotLag() - lagBefore,
		fpi:   b.walFPI() - fpiBefore,
		ckpt:  b.redoLSN() != redoBefore,
		lat:   lat,
		wall:  wall,
	}
}

func (b *cdcBench) slotLag() int64 {
	b.t.Helper()
	var lag int64
	require.NoError(b.t, b.admin.QueryRow(b.ctx,
		`SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)::bigint
		 FROM pg_replication_slots WHERE slot_name = $1`, cdcSlot).Scan(&lag))
	return lag
}

func (b *cdcBench) walFPI() int64 {
	b.t.Helper()
	var n int64
	require.NoError(b.t, b.admin.QueryRow(b.ctx, `SELECT wal_fpi FROM pg_stat_wal`).Scan(&n))
	return n
}

func (b *cdcBench) redoLSN() string {
	b.t.Helper()
	var lsn string
	require.NoError(b.t, b.admin.QueryRow(b.ctx, `SELECT redo_lsn::text FROM pg_control_checkpoint()`).Scan(&lsn))
	return lsn
}

func (b *cdcBench) waitCaughtUp(timeout time.Duration) time.Duration {
	b.t.Helper()
	start := time.Now()
	require.Eventually(b.t, func() bool { return b.slotLag() < walCaughtUp }, timeout, 500*time.Millisecond,
		"slot lag never dropped below 1MB")
	return time.Since(start)
}

// writeNoise generates WAL in a database Debezium does not capture.
func (b *cdcBench) writeNoise() int64 {
	b.t.Helper()
	base := b.currentLSN()
	_, err := b.admin.Exec(b.ctx, `CREATE DATABASE db_noise`)
	require.NoError(b.t, err)
	noise := b.openPool("postgres", "postgres", "db_noise")
	_, err = noise.Exec(b.ctx, `CREATE TABLE noise (id bigint, pad text)`)
	require.NoError(b.t, err)
	for b.currentLSN()-base < walNoiseBytes {
		_, err = noise.Exec(b.ctx, `INSERT INTO noise SELECT g, repeat('x', 1500) FROM generate_series(1, 10000) g`)
		require.NoError(b.t, err)
	}
	return b.currentLSN() - base
}

func (b *cdcBench) currentLSN() int64 {
	b.t.Helper()
	var lsn int64
	require.NoError(b.t, b.admin.QueryRow(b.ctx,
		`SELECT pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0')::bigint`).Scan(&lsn))
	return lsn
}

func (b *cdcBench) assertHeartbeatAdvancesSlot() {
	t := b.t
	t.Helper()
	written := b.writeNoise()
	lag := b.slotLag()
	t.Logf("noise wrote %d MB of WAL outside db_seckill, slot lag now %d MB", written>>20, lag>>20)
	require.Greater(t, lag, int64(walNoiseBytes/2), "noise did not show up as slot lag")

	start := time.Now()
	advanced := false
	for time.Since(start) < 60*time.Second {
		if lag = b.slotLag(); lag < walCaughtUp {
			advanced = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	t.Logf("heartbeat advance: ok=%v after %s, final lag %d bytes", advanced, elapsed, lag)
	require.True(t, advanced, "confirmed_flush_lsn did not advance past seckill-free WAL within 60s")

	msgs := b.consume(1<<20, 5*time.Second)
	for _, m := range msgs {
		require.NotContains(t, strings.ToLower(m.Key+string(m.Payload)), "heartbeat",
			"logical heartbeat message leaked into the order topic")
	}
	t.Logf("order topic carried %d message(s), none heartbeat", len(msgs))
}
