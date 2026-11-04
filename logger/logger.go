package logger

import (
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/klinoklaz/greenhouse/config"
)

var l atomic.Pointer[zap.Logger]

func Get() *zap.Logger {
	return l.Load()
}

func Init() {
	zc := &zap.Config{
		Level:             zap.NewAtomicLevelAt(zap.InfoLevel),
		DisableCaller:     true,
		DisableStacktrace: true,
		OutputPaths:       []string{config.Log().Location},
	}
	if config.Log().UseJSON {
		zc.Encoding = "json"
		zc.EncoderConfig = zap.NewProductionEncoderConfig()
	} else {
		zc.Encoding = "console"
		zc.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	}
	zc.EncoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder
	if zl, err := zc.Build(); err == nil {
		l.Store(zl)
	} else if Get() != nil {
		Get().Error("logger initialization failed", zap.Error(err))
	}
}
