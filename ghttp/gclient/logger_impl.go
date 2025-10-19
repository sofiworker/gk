package gclient

import (
	"log"
	"os"
	"sync"
)

type stdClientLogger struct {
	base *log.Logger
	once sync.Once
}

func newClientLogger() Logger {
	return &stdClientLogger{
		base: log.New(os.Stdout, "[gclient] ", log.LstdFlags|log.Lmicroseconds),
	}
}

func (l *stdClientLogger) ensure() {
	l.once.Do(func() {
		if l.base == nil {
			l.base = log.New(os.Stdout, "[gclient] ", log.LstdFlags|log.Lmicroseconds)
		}
	})
}

func (l *stdClientLogger) Errorf(format string, v ...any) {
	l.ensure()
	l.base.Printf("[ERROR] "+format, v...)
}

func (l *stdClientLogger) Warnf(format string, v ...any) {
	l.ensure()
	l.base.Printf("[WARN] "+format, v...)
}

func (l *stdClientLogger) Debugf(format string, v ...any) {
	l.ensure()
	l.base.Printf("[DEBUG] "+format, v...)
}
