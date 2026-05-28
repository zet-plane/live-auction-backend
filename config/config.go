package config

import (
	"fmt"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var cfg *GlobalConfig

// Config is an alias kept for the kernel.Engine field type.
type Config = GlobalConfig

func LoadConfig(path string, fns ...func(*GlobalConfig)) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		println("cannot find config file in path: " + path)
		os.Exit(1)
	}

	cfg = new(GlobalConfig)
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		println("Config Read failed: " + err.Error())
		os.Exit(1)
	}
	if err := viper.Unmarshal(cfg); err != nil {
		println("Config Unmarshal failed: " + err.Error())
		os.Exit(1)
	}

	viper.OnConfigChange(func(e fsnotify.Event) {
		println("Config fileHandle changed: ", e.Name)
		_ = viper.ReadInConfig()
		if err := viper.Unmarshal(cfg); err != nil {
			println("New Config fileHandle Parse Failed: ", e.Name)
			return
		}
		for _, fn := range fns {
			if fn != nil {
				fn(cfg)
			}
		}
	})
	viper.WatchConfig()
}

func GetConfig() *GlobalConfig {
	return cfg
}

func GenConfig(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exist, use -f to Force coverage", path)
	}
	data, err := yaml.Marshal(&GlobalConfig{})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	println("config file generate success in " + path)
	return nil
}

func (c *GlobalConfig) Address() string {
	return fmt.Sprintf("%s:%s", c.HTTP.Host, c.HTTP.Port)
}

func (c *GlobalConfig) DatabaseConnMaxLifetime() time.Duration {
	return parseDuration(c.Database.ConnMaxLifetime, 30*time.Minute)
}

func (c *GlobalConfig) AuthTokenTTL() time.Duration {
	return parseDuration(c.Auth.TokenTTL, 24*time.Hour)
}

func (c *GlobalConfig) ObservabilityMetricsInterval() time.Duration {
	return parseDuration(c.Observability.MetricsInterval, 15*time.Second)
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
