package server

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"

	"go.uber.org/zap"

	"github.com/klinoklaz/greenhouse/logger"
)

func Debug(port int) {
	addr := fmt.Sprintf("localhost:%d", port)
	logger.Get().Sugar().
		Infof("starting debug server at %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Get().Error("can not start debug server", zap.Error(err))
	}
}
