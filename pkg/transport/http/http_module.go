package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/peterouob/seckill_service/pkg/logger"
	"go.uber.org/fx"
)

type HTTPServerParams struct {
	fx.In
	Config   *config.Config
	Shutdown fx.Shutdowner
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

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			lis, err := net.Listen("tcp", p.Config.Service.HTTPAddr)
			if err != nil {
				logger.Error("HTTP listen failed", err)
				return err
			}

			logger.Logf("HTTP server listening on %s", lis.Addr())

			go func() {
				if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Logf("HTTP server error: %v", err)
					if shutdownErr := p.Shutdown.Shutdown(fx.ExitCode(1)); shutdownErr != nil {
						logger.Logf("fx shutdown failed: %v", shutdownErr)
					}
				}
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
