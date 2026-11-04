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
)

func main() {
	var configFile string
	var target string
	flag.StringVar(&configFile, "f", "", "Specify a config file")
	flag.StringVar(&target, "t", "", "Specify a target url")
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
	cache.Init()
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)
	server.ListenHTTP(shutdown)
}
