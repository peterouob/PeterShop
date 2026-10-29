package idgenerate

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/etcdtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func startEtcd(t *testing.T) *clientv3.Client {
	t.Helper()
	cli, stop, err := etcdtest.StartETCDEmbed()
	require.NoError(t, err)
	t.Cleanup(stop)
	return cli
}

func newEtcdSnowflake(t *testing.T, cli *clientv3.Client) *Snowflake {
	t.Helper()
	sf, err := New(context.Background(), NewEtcdCoordinator(cli, "/idgen-"+t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close(context.Background()) })
	return sf
}

func collect(t *testing.T, sfs []*Snowflake, perInstance int) []int64 {
	t.Helper()
	bucket := make([][]int64, perInstance)
	var wg sync.WaitGroup
	for i := range perInstance {
		wg.Go(func() {
			ids := make([]int64, 0, len(sfs))
			for _, sf := range sfs {
				id, err := sf.Next()
				assert.NoError(t, err)
				ids = append(ids, id)
			}
			bucket[i] = ids
		})
	}
	wg.Wait()
	return slices.Concat(bucket...)
}

func TestIntegration_TwoInstancesNoDuplicate(t *testing.T) {
	cli := startEtcd(t)
	a := newEtcdSnowflake(t, cli)
	b := newEtcdSnowflake(t, cli)

	ids := collect(t, []*Snowflake{a, b}, 100_000)
	uniq := slices.Compact(slices.Sorted(slices.Values(ids)))
	assert.Len(t, uniq, len(ids), "found duplicate ids")
}

func TestIntegration_RevokedInstanceStopsIssuing(t *testing.T) {
	cli := startEtcd(t)
	sf := newEtcdSnowflake(t, cli)

	lastID, err := sf.Next()
	require.NoError(t, err)

	ctx := context.Background()
	leases, err := cli.Leases(ctx)
	require.NoError(t, err)
	require.Len(t, leases.Leases, 1)
	_, err = cli.Revoke(ctx, leases.Leases[0].ID)
	require.NoError(t, err)

	// Timeline (T0 = last successful tick before revoke; worst case T0 = now):
	//   local lease lasts until T0 + localLease, reservation until T0 + reserveAhead;
	//   neither is extended after revoke.
	//   stop: whichever expires first; Next() checks it itself, no tick needed.
	//   recover: from stop, wait for the local lease to expire too -> next tick -> start
	//   (including up to maxWaitTime in wait).
	const slack = time.Second             // scheduling, etcd RPCs, -race overhead
	localLease := leaseTTL - safetyMargin // keepAlive: sendAt + ttl - safetyMargin
	stopAfter := min(reserveAhead, localLease)
	stopWithin := stopAfter + slack
	recoverWithin := (localLease - stopAfter) + tickInterval + maxWaitTime + slack
	poll := tickInterval / 10

	var stopErr error

	require.Eventually(t, func() bool {
		_, stopErr = sf.Next()
		if stopErr != nil {
			return true
		}
		return false
	}, stopWithin, poll)

	assert.False(t, sf.Ready())

	var recovered int64
	require.Eventually(t, func() bool {
		id, err := sf.Next()
		if err != nil {
			return false
		}
		recovered = id
		return true
	}, recoverWithin, poll)

	assert.Greater(t, recovered, lastID)
}
