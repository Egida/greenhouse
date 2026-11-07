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

	var err error
	if configFile == "" {
		err = config.LoadSimple(target)
	} else {
		err = config.LoadFile(configFile)
	}
	logger.Init()
	if err != nil {
		logger.Get().Fatal("invalid config", zap.Error(err))
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		logger.Get().Sugar().Infof(
			"greenhouse cache proxy %s", info.Main.Version)
	}

	if debugPort > 0 {
		go server.Debug(debugPort)
	}

	turnstile.Init()
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
