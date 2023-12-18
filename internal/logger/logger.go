package logger

import (
	"os"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"gopkg.in/yaml.v3"
	// "path/filepath"
)

type loggerConfig struct {
	Logger struct {
		Level int8 `yaml:"level"`
		Encoding string `yaml:"encoding"`
		OutputPath []string `yaml:"outputPath"`
	}
}

func InitLogger(cfgPath string) *zap.SugaredLogger {
	log.Println("Initiating logger...")
	
	cfg := readLoggerConfig(cfgPath)

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	config := zap.Config{
		Level:             zap.NewAtomicLevelAt(zapcore.Level(cfg.Logger.Level)),
        Development:       false,
        DisableCaller:     false,
        DisableStacktrace: false,
        Sampling:          nil,
        Encoding:          cfg.Logger.Encoding,
        EncoderConfig:     encoderCfg,
        OutputPaths: cfg.Logger.OutputPath,
        ErrorOutputPaths: []string{
            "stderr",
        },
	}
	return zap.Must(config.Build()).Sugar()
}

func readLoggerConfig(cfgPath string) *loggerConfig {
	f, err := os.Open(cfgPath)
	if err != nil {
		log.Fatalf("Cannot read config file because of: %s", err)
	}
	defer f.Close()

	cfg := &loggerConfig{}

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(cfg)
	if err != nil {
		log.Fatalf("Cannot parse app config file because of: %s", err)
	}
	return cfg
}