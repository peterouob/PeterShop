package transport

import (
	"context"
	"net"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func httpApp(t *testing.T, addr string) *fxtest.App {
	return fxtest.New(t,
		fx.NopLogger,
		fx.Supply(&config.Config{Service: config.Service{HTTPAddr: addr}}),
		HTTPServerModule,
		fx.Invoke(func(*gin.Engine) {}),
	)
}

func TestHTTPServerStartStop(t *testing.T) {
	app := httpApp(t, "127.0.0.1:0")
	app.RequireStart()
	app.RequireStop()
}

func TestHTTPServerStartFailsWhenPortTaken(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func(lis net.Listener) {
		err := lis.Close()
		assert.NoError(t, err, "failed to close listener")
	}(lis)

	app := httpApp(t, lis.Addr().String())
	if err := app.Start(context.Background()); err == nil {
		app.RequireStop()
		t.Fatal("expected start to fail on an occupied port")
	}
}
