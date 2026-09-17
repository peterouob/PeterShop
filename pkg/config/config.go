package config

import (
	"fmt"
	"net/url"
	"time"
)

type Config struct {
	Service   Service
	MySQL     MySQL
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

type MySQL struct {
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

func (m MySQL) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		m.User, url.QueryEscape(m.Password), m.Host, m.Port, m.Database, m.Params)
}

func (m MySQL) URL() string {
	return fmt.Sprintf("mysql://%s:%s@tcp(%s:%d)/%s?%s",
		m.User, url.QueryEscape(m.Password), m.Host, m.Port, m.Database, m.Params)
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
		MySQL: MySQL{
			Host:            l.str("MYSQL_HOST", "127.0.0.1"),
			Port:            l.int("MYSQL_PORT", 3306),
			User:            l.str("MYSQL_USER", "seckill"),
			Password:        l.secret(SectionMySQL, "MYSQL_PASSWORD"),
			Database:        l.secret(SectionMySQL, "MYSQL_DATABASE"),
			Params:          l.str("MYSQL_PARAMS", "charset=utf8mb4&parseTime=True&loc=Local"),
			MaxOpenConns:    l.int("MYSQL_MAX_OPEN_CONNS", 64),
			MaxIdleConns:    l.int("MYSQL_MAX_IDLE_CONNS", 16),
			ConnMaxLifetime: l.duration("MYSQL_CONN_MAX_LIFETIME", time.Hour),
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
