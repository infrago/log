package log

import (
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

type (
	Logs = []Log
	Log  struct {
		Time  int64  `json:"time"`
		Level Level  `json:"level"`
		Body  string `json:"body"`
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
		"time":    time.Unix(0, log.Time).Format("2006/01/02 15:04:05.000"),
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
