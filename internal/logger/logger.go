package logger

import (
	"os"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"gopkg.in/yaml.v3"
)

type loggerConfig struct {
	Logger struct {
		Level int8 `yaml:"level"`
		Encoding string `yaml:"encoding"`
		OutputPath []string `yaml:"outputPath"`
	}
}

func InitLogger(cfgPath string) (*zap.SugaredLogger) {
	log.Println("Initiating logger...")

	var encoding string
	var outPath []string
	var level int8
	
	cfg, err := readLoggerConfig(cfgPath)
	if err != nil {
		// use defaults
		log.Printf("Got error while reading logger config, will use default values. Error: %v\n", err)
		encoding = "json"
		outPath = []string{"stderr"}
		level = 0

	} else {
		encoding = cfg.Logger.Encoding
		outPath = cfg.Logger.OutputPath
		level = cfg.Logger.Level
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	config := zap.Config{
		Level:             zap.NewAtomicLevelAt(zapcore.Level(level)),
        Development:       false,
        DisableCaller:     false,
        DisableStacktrace: false,
        Sampling:          nil,
        Encoding:          encoding,
        EncoderConfig:     encoderCfg,
        OutputPaths: outPath,
        ErrorOutputPaths: []string{
            "stderr",
        },
	}
	return zap.Must(config.Build()).Sugar()
}

func readLoggerConfig(cfgPath string) (*loggerConfig, error) {
	f, err := os.Open(cfgPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &loggerConfig{}

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}