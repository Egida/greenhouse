package main

import (
	"flag"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"go.uber.org/zap"

	"github.com/klinoklaz/greenhouse/cache"
	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/logger"
	"github.com/klinoklaz/greenhouse/server"
	"github.com/klinoklaz/greenhouse/turnstile"
	"github.com/klinoklaz/greenhouse/util"
)

func main() {
	var configFile, target string
	var debugPort int
	flag.StringVar(&configFile, "f", "", "Specify a config file")
	flag.StringVar(&target, "t", "", "Specify a target url")
	flag.IntVar(&debugPort, "d", 0, "Expose a debug endpoint for pprof")
	flag.Parse()

	if configFile == "" {
		prepare(target, true, false)
	} else {
		prepare(configFile, false, false)
	}
	// handle systemctl reload
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	go func() {
		for range reload {
			prepare("", false, true)
		}
	}()

	if info, ok := debug.ReadBuildInfo(); ok {
		logger.Get().Sugar().Infof("GREENHOUSE %s", info.Main.Version)
	}

	if debugPort > 0 {
		go server.Debug(debugPort)
	}

	cache.Init()
	shutdown := make(chan os.Signal, 1)
	exit := make(chan util.Void)
	signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-shutdown
		logger.Get().Info("shutting down", zap.String("cause", sig.String()))
		close(exit) // broadcast exit event
	}()
	server.ListenHTTP(exit)
}

func prepare(targetOrFile string, isSimple, isReload bool) {
	var err error
	if isSimple {
		err = config.LoadSimple(targetOrFile)
	} else {
		err = config.LoadFile(targetOrFile)
	}

	logger.Init() // idempotent
	switch {
	case err != nil && isSimple:
		logger.Get().Fatal("invalid target", zap.Error(err))
	case err == nil && isSimple:
		logger.Get().Warn("hot reload isn't supported in simple mode")
	case err != nil && !isReload:
		logger.Get().Fatal("invalid config", zap.Error(err))
	case err == nil && isReload:
		logger.Get().Info("config reloaded")
	case err != nil && isReload:
		logger.Get().Warn(
			"reloading config failed, nothing changed", zap.Error(err))
		return
	}
	turnstile.Init() // idempotent
}
