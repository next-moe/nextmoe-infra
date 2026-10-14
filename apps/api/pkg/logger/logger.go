package logger

import (
	"log"
	"os"
)

type Logger struct {
	info *log.Logger
}

var defaultLogger *Logger

func init() {
	defaultLogger = New()
}

func New() *Logger {
	return &Logger{
		info: log.New(os.Stdout, "[INFO] ", log.Ldate|log.Ltime|log.Lshortfile),
	}
}

func (l *Logger) Info(format string, v ...any) {
	l.info.Printf(format, v...)
}

func Init(env string) {
	defaultLogger = New()
}

func Info(format string, v ...any) {
	defaultLogger.Info(format, v...)
}
