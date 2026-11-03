package util

import (
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/klinoklaz/greenhouse/config"
)

var logger atomic.Pointer[zap.Logger]

func Logger() *zap.Logger {
	return logger.Load()
}

func InitLogger() {
	zc := &zap.Config{
		Level:             zap.NewAtomicLevelAt(zap.InfoLevel),
		DisableCaller:     true,
		DisableStacktrace: true,
		OutputPaths:       []string{config.Log.Location},
	}
	if config.Log.UseJSON {
		zc.Encoding = "json"
		zc.EncoderConfig = zap.NewProductionEncoderConfig()
	} else {
		zc.Encoding = "console"
		zc.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	}
	zc.EncoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder
	if zl, err := zc.Build(); err == nil {
		logger.Store(zl)
	} else if Logger() != nil {
		Logger().Error("logger initialization failed", zap.Error(err))
	}
}
