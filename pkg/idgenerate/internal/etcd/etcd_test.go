package etcd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord/coordtest"
	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/etcdtest"
)

var testClient *clientv3.Client

func TestMain(m *testing.M) {
	cli, stop, err := etcdtest.StartETCDEmbed()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testClient = cli
	code := m.Run()
	stop()
	os.Exit(code)
}

func newCoord(t *testing.T) *Coordinator {
	return New(testClient, "/idgen-test/"+t.Name())
}

func TestContract(t *testing.T) {
	coordtest.RunGrantTest(t, func(t *testing.T) coord.Coordinator { return newCoord(t) })
	coordtest.Run(t, func(t *testing.T) coord.Coordinator { return newCoord(t) })
}

func TestLeaseExpiresForReal(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for a real lease expiry")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := newCoord(t)

	a, err := c.Grant(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.Claim(ctx, 1, a.ID, "a"); err != nil || !ok {
		t.Fatalf("claim a: ok=%v err=%v", ok, err)
	}
	if err := c.Reserve(ctx, 1, a.ID, 5000); err != nil {
		t.Fatal(err)
	}

	b, err := c.Grant(ctx, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	for {
		prev, ok, err := c.Claim(ctx, 1, b.ID, "b")
		if ok {
			if prev != 5000 {
				t.Fatalf("prev = %d, want 5000", prev)
			}
			if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
				t.Fatalf("lease expired too early: %s", elapsed)
			}
			break
		}
		if !errors.Is(err, coord.ErrKeyHadOccupied) {
			t.Fatalf("unexpected error: %v", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("lease did not expire")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
