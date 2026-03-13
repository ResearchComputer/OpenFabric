package common

import (
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.SugaredLogger

func init() {
	InitLogger()
}

func InitLogger() {
	var config zap.Config
	if viper.GetBool("production_logging") {
		config = zap.NewProductionConfig()
		config.Sampling = &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 10,
		}
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	var level zapcore.Level
	if viper.IsSet("loglevel") {
		err := level.UnmarshalText([]byte(viper.GetString("loglevel")))
		if err == nil {
			config.Level.SetLevel(level)
		} else {
			config.Level.SetLevel(zapcore.Level(viper.GetInt("loglevel")))
		}
	} else if viper.IsSet("log_level") {
		err := level.UnmarshalText([]byte(viper.GetString("log_level")))
		if err == nil {
			config.Level.SetLevel(level)
		} else {
			config.Level.SetLevel(zapcore.Level(viper.GetInt("log_level")))
		}
	} else {
		config.Level.SetLevel(zapcore.InfoLevel)
	}
	// fmt.Printf("Log level set to %s\n", config.Level.Level().String())
	zapLogger, err := config.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	// defer func() { _ = zapLogger.Sync() }()
	if err != nil {
		panic(err)
	}
	Logger = zapLogger.Sugar()
}

// Logs an error and panics
func ReportError(err error, msg string) {
	if err != nil {
		Logger.Error(msg, " error: ", err)
	}
}
