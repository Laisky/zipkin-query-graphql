package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Laisky/zap"

	zipkin_graphql "github.com/Laisky/zipkin-query-graphql"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/Laisky/go-utils"
)

// loadSettingsFromFile preserves the legacy single-YAML-file behavior without following include fields.
func loadSettingsFromFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(file); err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	return nil
}

func SetupSettings() {
	// mode
	if utils.Settings.GetBool("debug") {
		fmt.Println("run in debug mode")
		utils.Settings.Set("log-level", "debug")
	} else { // prod mode
		fmt.Println("run in prod mode")
	}

	// log
	level := utils.Settings.GetString("log-level")
	switch level {
	case "debug", "info", "warn", "error":
	default:
		panic("log level only be debug/info/warn/error")
	}
	if _, err := utils.CreateNewDefaultLogger("zipkin-query-graphql", level); err != nil {
		panic(err)
	}

	// clock
	utils.SetInternalClock(100 * time.Millisecond)

	// load configuration
	cfgDirPath := utils.Settings.GetString("config")
	if err := loadSettingsFromFile(cfgDirPath); err != nil {
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
