package transport

import (
	"context"
	"fmt"
	"net"
	"time"

	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpcrecovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpcctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	grpcopentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

type GrpcServerParams struct {
	fx.In
	Config   *config.Config
	Services []grpc.ServiceDesc `group:"grpc_services"`
}

type GrpcServerReady struct {
	C <-chan struct{}
}

func ProvideGrpcServer(lc fx.Lifecycle, p GrpcServerParams) (*grpc.Server, GrpcServerReady) {
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpcrecovery.UnaryServerInterceptor(),
			grpcctxtags.UnaryServerInterceptor(),
			grpcopentracing.UnaryServerInterceptor(),
			grpczap.UnaryServerInterceptor(zap.L()),
		),
	)

	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthSrv)

	reflection.Register(server)
	readyC := make(chan struct{})
	serveErr := make(chan error, 1)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			lis, err := net.Listen("tcp", p.Config.Service.GRPCAddr)
			if err != nil {
				logger.Error("grpc listen failed", err)
				return err
			}

			healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
			go func() {
				logger.Logf("gRPC server listening on %s", p.Config.Service.GRPCAddr)
				close(readyC)
				serveErr <- server.Serve(lis)
			}()
			select {
			case err := <-serveErr:
				return fmt.Errorf("grpc server failed to start: %w", err)
			case <-time.After(50 * time.Millisecond):
				return nil
			}
		},
		OnStop: func(ctx context.Context) error {
			logger.Log("gRPC server graceful stopping...")

			healthSrv.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

			stopC := make(chan struct{})

			go func() {
				server.GracefulStop()
				close(stopC)
			}()

			select {
			case <-stopC:
				logger.Log("gRPC server stopped gracefully")
				return nil
			case <-ctx.Done():
				logger.Log("gRPC graceful stop timeout, forcing stop")
				server.Stop()
				return nil
			}
		},
	})

	return server, GrpcServerReady{readyC}
}

var GrpcServerModule = fx.Module("grpc_server",
	fx.Provide(ProvideGrpcServer),
)
