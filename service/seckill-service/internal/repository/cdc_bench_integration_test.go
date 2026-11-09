//go:build integration && cdc

package repository

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterouob/seckill_service/pkg/event"
	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	cdcPassword   = "password"
	cdcSlot       = "seckill_outbox"
	kafkaImage    = "confluentinc/confluent-local:8.4.0" // = Apache Kafka 4.4, same as k8s; the tc kafka module only drives this image
	debeziumImage = "quay.io/debezium/server:3.7.1.Final"
	pgImage       = "postgres:17-alpine"
)

var deployDir = filepath.Join("..", "..", "..", "..", "deploy")

// cdcBench mirrors the minikube CDC path: real initdb.sh, real migrations,
// real application.properties, Debezium logging in as svc_cdc.
type cdcBench struct {
	t       *testing.T
	ctx     context.Context
	pg      *postgres.PostgresContainer
	admin   *pgxpool.Pool // superuser, for slot inspection only
	pool    *pgxpool.Pool // svc_seckill, what Reserve uses in production
	repo    *seckillRepoImpl
	brokers []string // host-mapped listener
	dbz     *testcontainers.DockerContainer
}

func newCDCBench(t *testing.T) *cdcBench {
	t.Helper()
	ctx := context.Background()
	b := &cdcBench{t: t, ctx: ctx}

	nw, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = nw.Remove(context.Background()) })

	b.startPostgres(nw)
	b.startKafka(nw)
	b.createTopics()
	b.startDebezium(nw)
	return b
}

func (b *cdcBench) startPostgres(nw *testcontainers.DockerNetwork) {
	pg, err := postgres.Run(b.ctx, pgImage,
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.WithInitScripts(filepath.Join(deployDir, "k8s", "base", "infra", "postgres", "initdb.sh")),
		testcontainers.WithEnv(map[string]string{
			"PGPASSWORD_user":    cdcPassword,
			"PGPASSWORD_seckill": cdcPassword,
			"PGPASSWORD_order":   cdcPassword,
			"PGPASSWORD_payment": cdcPassword,
			"PGPASSWORD_cdc":     cdcPassword,
		}),
		testcontainers.WithCmd("postgres", "-c", "wal_level=logical", "-c", "max_slot_wal_keep_size=1GB"),
		network.WithNetwork([]string{"postgres"}, nw),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(b.t, pg)
	require.NoError(b.t, err)
	b.pg = pg

	b.admin = b.openPool("postgres", "postgres", "db_seckill")
	b.migrate()
	b.pool = b.openPool("svc_seckill", cdcPassword, "db_seckill")
	b.repo = &seckillRepoImpl{pool: b.pool}
}

func (b *cdcBench) hostPort() string {
	host, err := b.pg.Host(b.ctx)
	require.NoError(b.t, err)
	port, err := b.pg.MappedPort(b.ctx, "5432/tcp")
	require.NoError(b.t, err)
	return net.JoinHostPort(host, port.Port())
}

func (b *cdcBench) dsn(scheme, user, password, db string) string {
	return fmt.Sprintf("%s://%s:%s@%s/%s?sslmode=disable", scheme, user, password, b.hostPort(), db)
}

func (b *cdcBench) openPool(user, password, db string) *pgxpool.Pool {
	pool, err := pgxpool.New(b.ctx, b.dsn("postgres", user, password, db))
	require.NoError(b.t, err)
	b.t.Cleanup(pool.Close)
	return pool
}

// migrate runs as svc_seckill, same as `make migrate-up-seckill`, so table
// ownership and the GRANT to svc_cdc behave like the cluster.
func (b *cdcBench) migrate() {
	src := "file://" + filepath.ToSlash(filepath.Join(deployDir, "migrations", "seckill"))
	m, err := migrate.New(src, b.dsn("pgx5", "svc_seckill", cdcPassword, "db_seckill"))
	require.NoError(b.t, err)
	defer func() { _, _ = m.Close() }()
	require.NoError(b.t, m.Up())
}

func (b *cdcBench) startKafka(nw *testcontainers.DockerNetwork) {
	k, err := kafka.Run(b.ctx, kafkaImage, network.WithNetwork([]string{"kafka"}, nw))
	testcontainers.CleanupContainer(b.t, k)
	require.NoError(b.t, err)
	b.brokers, err = k.Brokers(b.ctx)
	require.NoError(b.t, err)
}

// createTopics mirrors `make kafka-topics`.
func (b *cdcBench) createTopics() {
	admin, err := sarama.NewClusterAdmin(b.brokers, sarama.NewConfig())
	require.NoError(b.t, err)
	defer func() { _ = admin.Close() }()
	err = admin.CreateTopic(event.TopicOrderReserved, &sarama.TopicDetail{NumPartitions: 3, ReplicationFactor: 1}, false)
	if !errors.Is(err, sarama.ErrTopicAlreadyExists) {
		require.NoError(b.t, err)
	}
}

func (b *cdcBench) startDebezium(nw *testcontainers.DockerNetwork) {
	dbz, err := testcontainers.Run(b.ctx, debeziumImage,
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      filepath.Join(deployDir, "k8s", "base", "infra", "debezium", "application.properties"),
			ContainerFilePath: "/debezium/config/application.properties",
			FileMode:          0o644,
		}),
		testcontainers.WithEnv(map[string]string{"PGPASSWORD_cdc": cdcPassword}),
		testcontainers.WithExposedPorts("8080/tcp"),
		network.WithNetwork([]string{"debezium"}, nw),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/q/health/ready").WithPort("8080/tcp").WithStartupTimeout(90*time.Second)),
	)
	testcontainers.CleanupContainer(b.t, dbz)
	require.NoError(b.t, err)
	b.dbz = dbz
	b.t.Cleanup(b.dumpDebeziumErrorsOnFailure)
}

func (b *cdcBench) dumpDebeziumErrorsOnFailure() {
	if !b.t.Failed() {
		return
	}
	logs, err := b.dbz.Logs(context.Background())
	if err != nil {
		return
	}
	defer logs.Close()
	sc := bufio.NewScanner(logs)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if line := sc.Text(); strings.Contains(line, `"level":"ERROR"`) {
			b.t.Log("debezium:", line)
		}
	}
}

// stopDebezium stops the container and waits until PG sees the slot as inactive.
func (b *cdcBench) stopDebezium() {
	timeout := 10 * time.Second
	require.NoError(b.t, b.dbz.Stop(b.ctx, &timeout))
	require.Eventually(b.t, func() bool {
		var active bool
		err := b.admin.QueryRow(b.ctx,
			`SELECT active FROM pg_replication_slots WHERE slot_name = $1`, cdcSlot).Scan(&active)
		return err == nil && !active
	}, 15*time.Second, 200*time.Millisecond, "slot still active after stopping debezium")
}

func (b *cdcBench) restartDebezium() {
	require.NoError(b.t, b.dbz.Start(b.ctx))
}

// dropSlot simulates max_slot_wal_keep_size invalidating the slot. Debezium must be stopped.
func (b *cdcBench) dropSlot() {
	_, err := b.admin.Exec(b.ctx, `SELECT pg_drop_replication_slot($1)`, cdcSlot)
	require.NoError(b.t, err)
}

func (b *cdcBench) seedActivity(activityID int64, productID string, price int64, stock int) {
	start := time.Now().Add(-time.Minute)
	_, err := b.pool.Exec(b.ctx,
		`INSERT INTO seckill_activity (activity_id, product_id, price, start_at, end_at, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 2, now(), now())`,
		activityID, productID, price, start, start.Add(time.Hour))
	require.NoError(b.t, err)
	_, err = b.pool.Exec(b.ctx,
		`INSERT INTO activity_stock (activity_id, total, available, locked, sold) VALUES ($1, $2, $2, 0, 0)`,
		activityID, stock)
	require.NoError(b.t, err)
}

func (b *cdcBench) consume(want int, timeout time.Duration) []mq.Message {
	c, err := sarama.NewConsumer(b.brokers, sarama.NewConfig())
	require.NoError(b.t, err)
	defer func() { _ = c.Close() }()

	partitions, err := c.Partitions(event.TopicOrderReserved)
	require.NoError(b.t, err)

	got := make(chan mq.Message)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, p := range partitions {
		pc, err := c.ConsumePartition(event.TopicOrderReserved, p, sarama.OffsetOldest)
		require.NoError(b.t, err)
		go func() {
			defer func() {
				_ = pc.Close()
			}()

			for {
				select {
				case <-ctx.Done():
					return
				case m, ok := <-pc.Messages():
					if !ok {
						return
					}

					select {
					case <-ctx.Done():
						return
					case got <- mq.Message{Key: string(m.Key), Payload: m.Value}:
					}
				}
			}
		}()
	}

	var msgs []mq.Message
	deadline := time.After(timeout)
	for len(msgs) < want {
		select {
		case m := <-got:
			msgs = append(msgs, m)
		case <-deadline:
			b.t.Logf("consume: timeout after %s with %d/%d message(s)", timeout, len(msgs), want)
			return msgs
		}
	}
	b.t.Logf("consumed %d message(s) from %s", len(msgs), event.TopicOrderReserved)
	return msgs
}

func (b *cdcBench) invalidateSlot() {
	b.t.Helper()
	ctx := b.t.Context()

	exec := func(q string) {
		b.t.Helper()
		_, err := b.admin.Exec(ctx, q)
		require.NoError(b.t, err, q)
	}

	exec(`ALTER SYSTEM SET max_slot_wal_keep_size = '1MB'`)
	exec(`SELECT pg_reload_conf()`)

	b.t.Cleanup(func() {
		ctx := context.Background()
		_, _ = b.admin.Exec(ctx, `ALTER SYSTEM RESET max_slot_wal_keep_size`)
		_, _ = b.admin.Exec(ctx, `SELECT pg_reload_conf()`)
	})

	deadline := time.Now().Add(30 * time.Second)
	for {
		exec(`SELECT pg_logical_emit_message(false, 'burn', repeat('x', 1024*1024))`)
		exec(`SELECT pg_switch_wal()`)
		exec(`CHECKPOINT`)

		var status string
		err := b.admin.QueryRow(ctx,
			`SELECT wal_status FROM pg_replication_slots WHERE slot_name = $1`,
			cdcSlot).Scan(&status)
		require.NoError(b.t, err)
		if status == "lost" {
			b.t.Logf("slot %s invalidated (wal_status=lost)", cdcSlot)
			return
		}
		if time.Now().After(deadline) {
			b.t.Fatalf("slot need to be lost，wal_status=%s", status)
		}
	}
}
