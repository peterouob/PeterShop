package config

import (
	"fmt"
	"net/url"
	"time"
)

type Config struct {
	Service   Service
	Postgres  Postgres
	Redis     Redis
	Kafka     Kafka
	Etcd      Etcd
	JWT       JWT
	Upstreams Upstreams
}

type Service struct {
	Name     string
	GRPCAddr string
	HTTPAddr string
}

type Postgres struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	Params          string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

type Redis struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

type Kafka struct {
	Brokers      []string
	ClientID     string
	RequiredAcks int
	MaxRetries   int
	RetryBackoff time.Duration
	BatchSize    int
	FlushTimeout time.Duration
}

type Etcd struct {
	Endpoints   []string
	DialTimeout time.Duration
	LeaseTTL    time.Duration
}

type JWT struct {
	Secret string
	TTL    time.Duration
}

type Upstreams struct {
	User    string
	Seckill string
}

// DSN is the connection URL for gorm / pgx.
func (p Postgres) DSN() string { return p.url("postgres") }

// URL is the same connection for golang-migrate, whose pgx/v5 driver registers the pgx5 scheme.
func (p Postgres) URL() string { return p.url("pgx5") }

func (p Postgres) url(scheme string) string {
	u := url.URL{
		Scheme:   scheme,
		User:     url.UserPassword(p.User, p.Password),
		Host:     fmt.Sprintf("%s:%d", p.Host, p.Port),
		Path:     "/" + p.Database,
		RawQuery: p.Params,
	}
	return u.String()
}

func Load(serviceName string, sections ...Section) (*Config, error) {
	if serviceName == "" {
		return nil, ErrMissingServiceName
	}

	l := newLoader(sections)
	cfg := &Config{
		Service: Service{
			Name:     serviceName,
			GRPCAddr: l.str("GRPC_ADDR", ":50051"),
			HTTPAddr: l.str("HTTP_ADDR", ":8080"),
		},
		Postgres: Postgres{
			Host:            l.str("PGHOST", "127.0.0.1"),
			Port:            l.int("PGPORT", 5432),
			User:            l.secret(SectionPostgres, "PGUSER"),
			Password:        l.secret(SectionPostgres, "PGPASSWORD"),
			Database:        l.secret(SectionPostgres, "PGDATABASE"),
			Params:          l.str("PG_PARAMS", "sslmode=disable"),
			MaxOpenConns:    l.int("PG_MAX_OPEN_CONNS", 20),
			MaxIdleConns:    l.int("PG_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: l.duration("PG_CONN_MAX_LIFETIME", time.Hour),
		},
		Redis: Redis{
			Addr:     l.str("REDIS_ADDR", "127.0.0.1:6379"),
			Password: l.str("REDIS_PASSWORD", ""),
			DB:       l.int("REDIS_DB", 0),
			PoolSize: l.int("REDIS_POOL_SIZE", 64),
		},
		Kafka: Kafka{
			Brokers:      l.list("KAFKA_BROKERS", []string{"127.0.0.1:9092"}),
			ClientID:     l.str("KAFKA_CLIENT_ID", serviceName),
			RequiredAcks: l.int("KAFKA_REQUIRED_ACKS", -1),
			MaxRetries:   l.int("KAFKA_MAX_RETRIES", 5),
			RetryBackoff: l.duration("KAFKA_RETRY_BACKOFF", 500*time.Millisecond),
			BatchSize:    l.int("KAFKA_BATCH_SIZE", 100),
			FlushTimeout: l.duration("KAFKA_FLUSH_TIMEOUT", 200*time.Millisecond),
		},
		Etcd: Etcd{
			Endpoints:   l.list("ETCD_ENDPOINTS", []string{"127.0.0.1:2379"}),
			DialTimeout: l.duration("ETCD_DIAL_TIMEOUT", 5*time.Second),
			LeaseTTL:    l.duration("ETCD_LEASE_TTL", 10*time.Second),
		},
		JWT: JWT{
			Secret: l.secret(SectionJWT, "JWT_SECRET"),
			TTL:    l.duration("JWT_TTL", 2*time.Hour),
		},
		Upstreams: Upstreams{
			User:    l.str("UPSTREAM_USER_ADDR", "dns:///127.0.0.1:50051"),
			Seckill: l.str("UPSTREAM_SECKILL_ADDR", "dns:///127.0.0.1:50052"),
		},
	}

	if err := l.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}
