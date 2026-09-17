package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"go.uber.org/fx"
)

type HTTPServerParams struct {
	fx.In
	Config *config.Config
}

func ProvideHTTPServer(lc fx.Lifecycle, p HTTPServerParams) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	server := gin.New()

	server.Use(gin.Recovery(), gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/healthz"},
	}))

	server.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	srv := &http.Server{
		Addr:         p.Config.Service.HTTPAddr,
		Handler:      server,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				logger.Logf("HTTP server listening on %s", p.Config.Service.HTTPAddr)
				serverErrors <- srv.ListenAndServe()
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Log("HTTP server graceful stopping...")
			if err := srv.Shutdown(ctx); err != nil {
				logger.Error("HTTP server shutdown error: %v", err)
				return srv.Close()
			}
			logger.Log("HTTP server stopped gracefully")
			return nil
		},
	})
	return server
}

var HTTPServerModule = fx.Module("http_server",
	fx.Provide(ProvideHTTPServer),
)
