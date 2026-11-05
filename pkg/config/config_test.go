package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unsetAll(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

var secretKeys = []string{"PGUSER", "PGPASSWORD", "PGDATABASE", "JWT_SECRET"}

func TestLoad_EmptyServiceName(t *testing.T) {
	_, err := Load("")
	require.ErrorIs(t, err, ErrMissingServiceName)
}

func TestLoad_MissingSecretsReportedTogether(t *testing.T) {
	unsetAll(t, secretKeys...)

	_, err := Load("svc", SectionPostgres, SectionJWT)
	require.Error(t, err)
	for _, key := range secretKeys {
		assert.Contains(t, err.Error(), key, "every missing secret must be listed in one error, not one per restart")
	}
}

func TestLoad_SecretsOfUnneededSectionsAreOptional(t *testing.T) {
	unsetAll(t, secretKeys...)
	t.Setenv("REDIS_ADDR", "redis:6379")

	_, err := Load("svc", SectionRedis)
	require.NoError(t, err, "a service that does not use postgres/jwt must not fail on their secrets")
}

func TestLoad_WhitespaceOnlySecretIsMissing(t *testing.T) {
	unsetAll(t, secretKeys...)
	t.Setenv("JWT_SECRET", "   ")

	_, err := Load("svc", SectionJWT)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET")
}

func TestLoad_BadValueNamesTheKey(t *testing.T) {
	cases := []struct{ key, value string }{
		{"PGPORT", "abc"},
		{"JWT_TTL", "two-hours"},
		{"KAFKA_BROKERS", " , "},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)

			_, err := Load("svc")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.key)
		})
	}
}

func TestLoad_BadValuesReportedTogether(t *testing.T) {
	t.Setenv("PGPORT", "abc")
	t.Setenv("JWT_TTL", "two-hours")

	_, err := Load("svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PGPORT")
	assert.Contains(t, err.Error(), "JWT_TTL")
}

func TestLoad_ListTrimsEntries(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", " k1:9092 , ,k2:9092 ")

	cfg, err := Load("svc")
	require.NoError(t, err)
	assert.Equal(t, []string{"k1:9092", "k2:9092"}, cfg.Kafka.Brokers)
}

func TestLoad_PostgresURLEscapesPassword(t *testing.T) {
	p := Postgres{Host: "db", Port: 5432, User: "svc_seckill", Password: "p@ss/w:rd", Database: "db_seckill", Params: "sslmode=disable"}

	assert.Equal(t, "postgres://svc_seckill:p%40ss%2Fw%3Ard@db:5432/db_seckill?sslmode=disable", p.DSN())
	assert.Equal(t, "pgx5://svc_seckill:p%40ss%2Fw%3Ard@db:5432/db_seckill?sslmode=disable", p.URL())
}

var addrKeys = []string{"PGHOST", "REDIS_ADDR", "KAFKA_BROKERS", "ETCD_ENDPOINTS", "UPSTREAM_USER_ADDR", "UPSTREAM_SECKILL_ADDR"}

func setSecrets(t *testing.T) {
	t.Helper()
	for _, k := range secretKeys {
		t.Setenv(k, "x")
	}
}

func TestLoad_MissingAddrsReportedTogether(t *testing.T) {
	unsetAll(t, addrKeys...)

	_, err := Load("svc", SectionRedis, SectionKafka)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REDIS_ADDR")
	assert.Contains(t, err.Error(), "KAFKA_BROKERS")
}

func TestLoad_EachSectionRequiresItsAddr(t *testing.T) {
	cases := []struct {
		section Section
		key     string
	}{
		{SectionPostgres, "PGHOST"},
		{SectionRedis, "REDIS_ADDR"},
		{SectionKafka, "KAFKA_BROKERS"},
		{SectionEtcd, "ETCD_ENDPOINTS"},
	}
	for _, tc := range cases {
		t.Run(string(tc.section), func(t *testing.T) {
			unsetAll(t, addrKeys...)
			setSecrets(t)

			_, err := Load("svc", tc.section)
			require.Error(t, err, "%s unset must fail instead of silently dialing localhost", tc.key)
			assert.Contains(t, err.Error(), tc.key)
		})
	}
}

func TestLoad_AddrFromEnv(t *testing.T) {
	setSecrets(t)
	t.Setenv("PGHOST", "postgres")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("KAFKA_BROKERS", "kafka-0:9092,kafka-1:9092")
	t.Setenv("ETCD_ENDPOINTS", "etcd:2379")
	t.Setenv("UPSTREAM_USER_ADDR", "dns:///user-service:50051")
	t.Setenv("UPSTREAM_SECKILL_ADDR", "dns:///seckill-service:50052")

	cfg, err := Load("svc", SectionPostgres, SectionRedis, SectionKafka, SectionEtcd, SectionUpstreams)
	require.NoError(t, err)
	assert.Equal(t, "postgres", cfg.Postgres.Host)
	assert.Equal(t, "redis:6379", cfg.Redis.Addr)
	assert.Equal(t, []string{"kafka-0:9092", "kafka-1:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, []string{"etcd:2379"}, cfg.Etcd.Endpoints)
	assert.Equal(t, "dns:///user-service:50051", cfg.Upstreams.User)
	assert.Equal(t, "dns:///seckill-service:50052", cfg.Upstreams.Seckill)
}

func TestLoad_UnneededAddrsHaveNoLocalhostFallback(t *testing.T) {
	unsetAll(t, addrKeys...)

	cfg, err := Load("svc")
	require.NoError(t, err, "a service must not fail on addresses of sections it does not use")
	assert.Empty(t, cfg.Postgres.Host)
	assert.Empty(t, cfg.Redis.Addr)
	assert.Empty(t, cfg.Kafka.Brokers)
	assert.Empty(t, cfg.Etcd.Endpoints)
	assert.Empty(t, cfg.Upstreams.User)
	assert.Empty(t, cfg.Upstreams.Seckill)
}

func TestLoad_UpstreamsSectionRequiresEveryUpstream(t *testing.T) {
	unsetAll(t, addrKeys...)

	_, err := Load("svc", SectionUpstreams)
	require.Error(t, err, "api-gateway must not silently dial localhost for its upstreams")
	assert.Contains(t, err.Error(), "UPSTREAM_USER_ADDR")
	assert.Contains(t, err.Error(), "UPSTREAM_SECKILL_ADDR")
}

func TestLoad_ZeroConnectTimeoutRejected(t *testing.T) {
	t.Setenv("PG_CONNECT_TIMEOUT", "0s")

	_, err := Load("svc")
	require.Error(t, err, "0 would mean wait forever for a connection")
	assert.Contains(t, err.Error(), "PG_CONNECT_TIMEOUT")
}

func TestLoad_PostgresPoolSizeValidated(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		key  string
	}{
		{"max zero", map[string]string{"PG_MAX_OPEN_CONNS": "0"}, "PG_MAX_OPEN_CONNS"},
		{"min above max", map[string]string{"PG_MAX_OPEN_CONNS": "5", "PG_MIN_CONNS": "6"}, "PG_MIN_CONNS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := Load("svc")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.key)
		})
	}
}
