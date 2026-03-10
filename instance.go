package log

import (
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/infrago/base"
)

type Instance struct {
	connect Connection

	Name    string
	Config  Config
	Setting map[string]any

	formatOnce  sync.Once
	formatParts []formatPart
}

const logTimeFormat = "2006-01-02 15:04:05.000000"

func (inst *Instance) Format(entry Log) string {
	if inst.Config.Json {
		payload := map[string]any{
			"time":    entry.Time.Format(logTimeFormat),
			"unix":    entry.Time.Unix(),
			"nano":    entry.Time.UnixNano(),
			"level":   levelStrings[entry.Level],
			"flag":    inst.Config.Flag,
			"body":    entry.Body,
			"project": entry.Project,
			"role":    entry.Role,
			"profile": entry.Profile,
			"node":    entry.Node,
		}
		if len(entry.Fields) > 0 {
			payload["fields"] = entry.Fields
		}
		body, _ := json.Marshal(payload)
		return string(body)
	}

	message := inst.formatText(entry)
	if len(entry.Fields) > 0 {
		message += " " + formatFields(entry.Fields)
	}

	return message
}

func (inst *Instance) Allow(level Level, body, project, role, profile, node string, fields Map) bool {
	if !inst.Config.Levels[level] {
		return false
	}
	r := inst.Config.Sample
	if r >= 1 {
		return true
	}
	if r <= 0 {
		return false
	}
	return hash01(level, inst.Name, body, project, role, profile, node, fields) <= r
}

func normalizeLevels(cfg Config) Config {
	if len(cfg.Levels) > 0 {
		return cfg
	}

	cfg.Levels = map[Level]bool{}
	for level := range levelStrings {
		if level <= cfg.Level {
			cfg.Levels[level] = true
		}
	}
	return cfg
}

func normalizeConfig(cfg Config) Config {
	if cfg.Driver == "" {
		cfg.Driver = "default"
	}
	if cfg.Levels == nil {
		cfg.Levels = map[Level]bool{}
	}
	if cfg.Level < LevelFatal || cfg.Level > LevelDebug {
		cfg.Level = LevelDebug
	}
	if cfg.Format == "" {
		cfg.Format = "%time% [%level%] %body%"
	}
	if cfg.Buffer <= 0 {
		cfg.Buffer = 1024
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 512
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = time.Millisecond * 200
	}
	if cfg.Overflow == "" {
		cfg.Overflow = OverflowBlock
	}
	if cfg.Drop == "" {
		cfg.Drop = DropOld
	}
	switch cfg.Overflow {
	case OverflowDropNewest, OverflowDropOldest, OverflowBlock, OverflowDrop:
	default:
		cfg.Overflow = OverflowBlock
	}
	switch cfg.Drop {
	case DropOld, DropNew:
	default:
		cfg.Drop = DropOld
	}
	if cfg.Sample <= 0 {
		cfg.Sample = 1
	}
	if cfg.Sample > 1 {
		cfg.Sample = 1
	}
	cfg = normalizeLevels(cfg)
	return cfg
}

func hash01(level Level, name, body, project, role, profile, node string, fields Map) float64 {
	h := fnv.New64a()
	hashWrite(h, strconv.Itoa(level))
	hashWrite(h, ":")
	hashWrite(h, name)
	hashWrite(h, ":")
	hashWrite(h, body)
	hashWrite(h, ":")
	hashWrite(h, project)
	hashWrite(h, ":")
	hashWrite(h, role)
	hashWrite(h, ":")
	hashWrite(h, profile)
	hashWrite(h, ":")
	hashWrite(h, node)
	if len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			hashWrite(h, "|")
			hashWrite(h, k)
			hashWrite(h, "=")
			hashWrite(h, fmt.Sprint(fields[k]))
		}
	}
	v := h.Sum64()
	return float64(v%1000000) / 1000000.0
}

func formatFields(fields Map) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(fields[k]))
	}
	return b.String()
}

type formatToken int

const (
	tokenLiteral formatToken = iota
	tokenNano
	tokenUnix
	tokenTime
	tokenName
	tokenFlag
	tokenLevel
	tokenBody
	tokenProject
	tokenRole
	tokenProfile
	tokenNode
)

type formatPart struct {
	token formatToken
	text  string
}

var formatTokens = []struct {
	key   string
	token formatToken
}{
	{"%nano%", tokenNano},
	{"%unix%", tokenUnix},
	{"%time%", tokenTime},
	{"%name%", tokenName},
	{"%flag%", tokenFlag},
	{"%level%", tokenLevel},
	{"%body%", tokenBody},
	{"%project%", tokenProject},
	{"%role%", tokenRole},
	{"%profile%", tokenProfile},
	{"%node%", tokenNode},
}

func (inst *Instance) formatText(entry Log) string {
	inst.formatOnce.Do(func() {
		message := inst.Config.Format
		if message == "" {
			message = "%time% [%level%] %body%"
		}
		inst.formatParts = compileFormat(message)
	})

	var b strings.Builder
	b.Grow(len(inst.Config.Format) + len(entry.Body) + 64)
	for _, part := range inst.formatParts {
		switch part.token {
		case tokenLiteral:
			b.WriteString(part.text)
		case tokenNano:
			b.WriteString(strconv.FormatInt(entry.Time.UnixNano(), 10))
		case tokenUnix:
			b.WriteString(strconv.FormatInt(entry.Time.Unix(), 10))
		case tokenTime:
			b.WriteString(entry.Time.Format(logTimeFormat))
		case tokenName:
			b.WriteString(inst.Name)
		case tokenFlag:
			b.WriteString(inst.Config.Flag)
		case tokenLevel:
			b.WriteString(levelStrings[entry.Level])
		case tokenBody:
			b.WriteString(entry.Body)
		case tokenProject:
			b.WriteString(entry.Project)
		case tokenRole:
			b.WriteString(entry.Role)
		case tokenProfile:
			b.WriteString(entry.Profile)
		case tokenNode:
			b.WriteString(entry.Node)
		}
	}
	return b.String()
}

func compileFormat(format string) []formatPart {
	if format == "" {
		return []formatPart{{token: tokenLiteral, text: "%time% [%level%] %body%"}}
	}

	parts := make([]formatPart, 0, 8)
	for len(format) > 0 {
		idx := strings.IndexByte(format, '%')
		if idx < 0 {
			parts = append(parts, formatPart{token: tokenLiteral, text: format})
			break
		}
		if idx > 0 {
			parts = append(parts, formatPart{token: tokenLiteral, text: format[:idx]})
			format = format[idx:]
		}

		matched := false
		for _, item := range formatTokens {
			if strings.HasPrefix(format, item.key) {
				parts = append(parts, formatPart{token: item.token})
				format = format[len(item.key):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		parts = append(parts, formatPart{token: tokenLiteral, text: "%"})
		format = format[1:]
	}
	return parts
}

func hashWrite(h hash.Hash64, text string) {
	_, _ = io.WriteString(h, text)
}
