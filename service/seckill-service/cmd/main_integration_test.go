//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/peterouob/seckill_service/pkg/etcd/etcdtest"
	"github.com/peterouob/seckill_service/pkg/idgenerate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func startEtcd(t *testing.T) *clientv3.Client {
	t.Helper()
	cli, stop, err := etcdtest.StartETCDEmbed()
	require.NoError(t, err)
	t.Cleanup(stop)
	return cli
}

func TestAppStarts(t *testing.T) {
	client := startEtcd(t)
	assert.NotNil(t, client)

	var sf *idgenerate.Snowflake
	app := fxtest.New(t,
		fx.NopLogger,
		fx.Supply(client),
		idgenerate.SnowflakeProvider,
		fx.Populate(&sf))

	app.RequireStart()
	kvs := getWorkerKeys(t, client)
	require.Len(t, kvs, 1)
	require.NotZero(t, kvs[0].Lease)

	id1, err := sf.Next()
	require.NoError(t, err)
	assert.False(t, id1 <= 0)

	id2, err := sf.Next()
	require.NoError(t, err)
	assert.False(t, id2 <= 0)

	assert.Greater(t, id2, id1)

	app.RequireStop()

	require.Empty(t, getWorkerKeys(t, client))

	_, err = sf.Next()
	require.Error(t, err)
}

func getWorkerKeys(t *testing.T, cli *clientv3.Client) []*mvccpb.KeyValue {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := cli.Get(ctx, "/idgen/workers", clientv3.WithPrefix())
	require.NoError(t, err)
	return resp.Kvs
}
