package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logg *zap.Logger
	once sync.Once
)

func InitLogger(serviceName ...string) {
	name := "app"
	if len(serviceName) > 0 && serviceName[0] != "" {
		name = serviceName[0]
	}

	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalLevelEncoder

	syncers := []zapcore.WriteSyncer{zapcore.AddSync(os.Stdout)}
	file, err := openLogFile(name)
	if file != nil {
		syncers = append(syncers, zapcore.AddSync(file))
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(cfg),
		zapcore.NewMultiWriteSyncer(syncers...),
		zapcore.DebugLevel,
	)
	logg = zap.New(core, zap.AddCaller()).With(zap.String("service", name))

	if err != nil {
		logg.Warn("file logging disabled", zap.Error(err))
	}
}

func openLogFile(serviceName string) (*os.File, error) {
	dir := os.Getenv("LOG_DIR")
	if dir == "" {
		return nil, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, fmt.Sprintf("log-%s-%s.log", serviceName, time.Now().Format("2006-01-02")))
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}
	return file, nil
}

func getLogger() *zap.Logger {
	once.Do(func() {
		if logg == nil {
			InitLogger()
		}
	})
	return logg
}

func Log(msg any) {
	getLogger().Info(fmt.Sprint(msg))
}

func Logf(format string, args ...any) {
	getLogger().Info(fmt.Sprintf(format, args...))
}

func Warn(msg any) {
	getLogger().Warn(fmt.Sprint(msg))
}

func Error(msg string, err error, fields ...zap.Field) {
	getLogger().Error(msg, append(fields, zap.Error(err))...)
}

func Errorf(err error, format string, args ...any) {
	getLogger().Error(fmt.Sprintf(format, args...), zap.Error(err))
}
