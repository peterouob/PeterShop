package transport

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

func NewConn(lc fx.Lifecycle, target string) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, fmt.Errorf("grpc: upstream target must not be empty")
	}

	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableServiceConfig(),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff:           backoff.DefaultConfig,
			MinConnectTimeout: 5 * time.Second,
		}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc: create client for %s: %w", target, err)
	}

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			conn.Connect()
			return nil
		},
		OnStop: func(context.Context) error {
			return conn.Close()
		},
	})
	return conn, nil
}
