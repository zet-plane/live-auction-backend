package config

type GlobalConfig struct {
	Mode     string   `yaml:"mode"     mapstructure:"mode"`
	App      App      `yaml:"app"      mapstructure:"app"`
	HTTP     HTTP     `yaml:"http"     mapstructure:"http"`
	Database Database `yaml:"database" mapstructure:"database"`
	Redis    Redis    `yaml:"redis"    mapstructure:"redis"`
	Auth     Auth     `yaml:"auth"     mapstructure:"auth"`
	Auction  Auction  `yaml:"auction"  mapstructure:"auction"`
}

type App struct {
	Name    string `yaml:"name"    mapstructure:"name"`
	Version string `yaml:"version" mapstructure:"version"`
}

type HTTP struct {
	Host string `yaml:"host" mapstructure:"host"`
	Port string `yaml:"port" mapstructure:"port"`
}

type Database struct {
	Driver          string `yaml:"driver"            mapstructure:"driver"`
	DSN             string `yaml:"dsn"               mapstructure:"dsn"`
	MaxIdleConns    int    `yaml:"max_idle_conns"    mapstructure:"max_idle_conns"`
	MaxOpenConns    int    `yaml:"max_open_conns"    mapstructure:"max_open_conns"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime" mapstructure:"conn_max_lifetime"`
}

type Redis struct {
	Addr     string `yaml:"addr"     mapstructure:"addr"`
	Password string `yaml:"password" mapstructure:"password"`
	DB       int    `yaml:"db"       mapstructure:"db"`
}

type Auth struct {
	TokenSecret string `yaml:"token_secret" mapstructure:"token_secret"`
	TokenTTL    string `yaml:"token_ttl"    mapstructure:"token_ttl"`
}

type Auction struct {
	ExtendTriggerSec  int `yaml:"extend_trigger_sec"    mapstructure:"extend_trigger_sec"`
	AutoExtendSec     int `yaml:"auto_extend_sec"       mapstructure:"auto_extend_sec"`
	MaxExtendCount    int `yaml:"max_extend_count"      mapstructure:"max_extend_count"`
	MaxTotalExtendSec int `yaml:"max_total_extend_sec"  mapstructure:"max_total_extend_sec"`
}
