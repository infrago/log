package log

import (
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

type (
	Logs = []Log
	Log  struct {
		Time  time.Time `json:"time"`
		Level Level     `json:"level"`
		Body  string    `json:"body"`
	}
)

// Log Mapping
func (log *Log) Mapping() Map {
	return Map{
		"id":      infra.Generate(),
		"name":    infra.Name(),
		"role":    infra.Role(),
		"version": infra.Version(),
		"level":   levelStrings[log.Level],
		"body":    log.Body,
		"time":    log.Time,
	}
}

// Logs Mapping
// func (logs Logs) Mapping() []Map {
// 	objs := make([]Map, 0)
// 	for _, log := range logs {
// 		objs = append(objs, log.Mapping())
// 	}
// 	return objs
// }
