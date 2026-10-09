package main

import (
	utils "github.com/Laisky/go-utils"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConsumerStartupSettings protects single-file loading and the configured clock interval.
func TestConsumerStartupSettings(t *testing.T) {
	keys := []string{"debug", "log-level", "config", "settings.secret", "include"}
	previous := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		previous[key] = utils.Settings.Get(key)
	}
	previousLogger := utils.Logger
	previousInterval := utils.Clock.Interval()
	t.Cleanup(func() {
		for _, key := range keys {
			utils.Settings.Set(key, previous[key])
		}
		utils.Logger = previousLogger
		utils.SetInternalClock(previousInterval)
	})
	file := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(file, []byte("settings:\n  secret: synthetic-startup-key\ninclude: deliberately-missing.yml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	utils.Settings.Set("debug", false)
	utils.Settings.Set("log-level", "error")
	utils.Settings.Set("config", file)
	utils.Settings.Set("settings.secret", nil)
	SetupSettings()
	if utils.Settings.GetString("settings.secret") != "synthetic-startup-key" {
		t.Fatal("single-file settings were not loaded")
	}
	if utils.Clock.Interval() != 100*time.Millisecond {
		t.Fatal("clock interval changed")
	}
}
