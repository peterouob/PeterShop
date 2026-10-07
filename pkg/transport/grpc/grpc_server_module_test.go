package transport

import (
	"context"
	"net"
	"testing"

	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"google.golang.org/grpc"
)

func grpcApp(t *testing.T, addr string) *fxtest.App {
	return fxtest.New(t,
		fx.NopLogger,
		fx.Supply(&config.Config{Service: config.Service{GRPCAddr: addr}}),
		GrpcServerModule,
		fx.Invoke(func(*grpc.Server) {}),
	)
}

func TestGrpcServerStartStop(t *testing.T) {
	app := grpcApp(t, "127.0.0.1:0")
	app.RequireStart()
	app.RequireStop()
}

func TestGrpcServerStartFailsWhenPortTaken(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	defer func(lis net.Listener) {
		err := lis.Close()
		assert.NoError(t, err, "failed to close listener")
	}(lis)

	app := grpcApp(t, lis.Addr().String())
	if err := app.Start(context.Background()); err == nil {
		app.RequireStop()
		t.Fatal("expected start to fail on an occupied port")
	}
}
