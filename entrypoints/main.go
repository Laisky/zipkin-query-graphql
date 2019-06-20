package main

import (
	"fmt"
	"time"

	"github.com/Laisky/zap"

	zipkin_graphql "github.com/Laisky/zipkin-query-graphql"

	"github.com/spf13/pflag"

	"github.com/Laisky/go-utils"
)

func SetupSettings() {
	// mode
	if utils.Settings.GetBool("debug") {
		fmt.Println("run in debug mode")
		utils.Settings.Set("log-level", "debug")
	} else { // prod mode
		fmt.Println("run in prod mode")
	}

	// log
	utils.SetupLogger(utils.Settings.GetString("log-level"))

	// clock
	utils.SetupClock(100 * time.Millisecond)

	// load configuration
	cfgDirPath := utils.Settings.GetString("config")
	if err := utils.Settings.SetupFromFile(cfgDirPath); err != nil {
		utils.Logger.Panic("can not load config from disk",
			zap.String("dirpath", cfgDirPath))
	} else {
		utils.Logger.Info("success load configuration from dir",
			zap.String("dirpath", cfgDirPath))
	}
}

func SetupArgs() {
	pflag.Bool("debug", false, "run in debug mode")
	pflag.Bool("dry", false, "run in dry mode")
	pflag.String("addr", "localhost:8090", "default `localhost:8090`")
	pflag.String("span-url-prefix", "http://zipkin-server.pro.ptcloud.t.home/zipkin/traces/", "prefix to generate span url")
	pflag.String("config", "/etc/go-zipkin-query/settings.yml", "config file path")
	pflag.String("log-level", "info", "`debug/info/error`")
	pflag.Int("heartbeat", 60, "heartbeat seconds")
	pflag.Parse()
	utils.Settings.BindPFlags(pflag.CommandLine)
}

func main() {
	defer utils.Logger.Sync()
	SetupArgs()
	SetupSettings()
	zipkin_graphql.SetupESCli(utils.Settings.GetString("settings.esapi"))

	zipkin_graphql.RunServer(utils.Settings.GetString("addr"))
}
