package log

import (
	"strconv"
	"strings"
	"time"

	. "github.com/infrago/base"
)

type (
	Instance struct {
		connect Connect

		Name    string
		Config  Config
		Setting Map
	}
)

// Log format
func (this *Instance) Format(log Log) string {
	message := this.Config.Format

	message = strings.Replace(message, "%nano%", strconv.FormatInt(log.Time, 10), -1)
	message = strings.Replace(message, "%time%", time.Unix(0, log.Time).Format("2006/01/02 15:04:05.000"), -1)
	message = strings.Replace(message, "%role%", infra.Role(), -1)
	message = strings.Replace(message, "%node%", infra.Node(), -1)
	message = strings.Replace(message, "%flag%", this.Config.Flag, -1)
	message = strings.Replace(message, "%level%", levelStrings[log.Level], -1)
	// message = strings.Replace(message, "%file%", log.File, -1)
	// message = strings.Replace(message, "%line%", strconv.Itoa(log.Line), -1)
	// message = strings.Replace(message, "%func%", log.Func, -1)
	message = strings.Replace(message, "%body%", log.Body, -1)

	return message
}

// Log Mapping
func (this *Instance) Mapping(log Log) Map {
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

// Log Mapping
func (this *Instance) Mappings(logs Logs) []Map {

	docs := []Map{}
	for _, log := range logs {
		docs = append(docs, this.Mapping(log))
	}

	return docs
}
