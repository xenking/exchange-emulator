package config

import (
	"time"

	"github.com/cristalhq/aconfig"
	"github.com/cristalhq/aconfig/aconfigyaml"
)

// ApplicationVersion represents the version of current application.
var ApplicationVersion string

// Config is a structure for values of the environment variables.
type Config struct {
	GracefulShutdownDelay time.Duration `default:"30s"`

	App      ApplicationConfig
	Exchange ExchangeConfig
	Parser   ParserConfig
	Log      LoggerConfig
	WS       WSConfig
	GRPC     GRPCConfig
}

type ApplicationConfig struct {
	Version string `default:"v0.0.1"`
	Name    string `default:"exchange-emulator"`
}

type ExchangeConfig struct {
	InfoFile   string  `default:"./data/exchange.json"`
	Commission float64 `default:"0.1"`
}

type WSConfig struct {
	OrdersAddr string `default:":8000"`
	PricesAddr string `default:":8000"`
}

type GRPCConfig struct {
	Addr        string `default:"8000"`
	DisableAuth bool   `default:"false"`
}

type LoggerConfig struct {
	Level      string `default:"debug"`
	WithCaller int    `default:"1"`
}

type ParserConfig struct {
	File   string
	Delay  time.Duration `default:"3ms"`
	Offset int64         `default:"0"`
}

// NewConfig loads values from environment variables and returns loaded configuration.
func NewConfig(file string) (*Config, error) {
	config := &Config{}
	loader := aconfig.LoaderFor(config, aconfig.Config{
		SkipFlags:        true,
		EnvPrefix:        "",
		AllowUnknownEnvs: true,
		AllFieldRequired: true,
		Files:            []string{file},
		FileDecoders: map[string]aconfig.FileDecoder{
			".yml": aconfigyaml.New(),
		},
	})
	if err := loader.Load(); err != nil {
		return nil, err
	}
	if config.App.Version == "v0.0.1" || config.App.Version == "" {
		config.App.Version = ApplicationVersion
	}

	return config, nil
}
