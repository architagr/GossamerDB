package log

import (
	dbCtx "GossamerDB/internal/context"
	"log"
	"sync"
)

type logger struct {
	log *log.Logger
}

var (
	logObj *logger
	once   sync.Once
)

func GetLogger() *logger {
	once.Do(func() {
		logObj = &logger{
			log: log.Default(),
		}
	})
	return logObj
}

func (l *logger) Info(ctx dbCtx.Context, message string) {
	l.log.Println(message)

}
func (l *logger) Debug(ctx dbCtx.Context, message string) {
	l.log.Println(message)
}

func (l *logger) Error(ctx dbCtx.Context, err error, message string) {
	l.log.Println(err, message)
}
