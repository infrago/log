package log

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/bamgoo/base"
)

type Instance struct {
	connect Connection

	Name    string
	Config  Config
	Setting map[string]any
}

func (inst *Instance) Format(entry Log) string {
	if inst.Config.Json {
		payload := map[string]any{
			"time":  entry.Time.Format("2006-01-02 15:04:05.000"),
			"unix":  entry.Time.Unix(),
			"nano":  entry.Time.UnixNano(),
			"level": levelStrings[entry.Level],
			"name":  inst.Name,
			"flag":  inst.Config.Flag,
			"body":  entry.Body,
		}
		if len(entry.Fields) > 0 {
			payload["fields"] = entry.Fields
		}
		body, _ := json.Marshal(payload)
		return string(body)
	}

	message := inst.Config.Format
	if message == "" {
		message = "%time% [%level%] %body%"
	}

	message = strings.ReplaceAll(message, "%nano%", strconv.FormatInt(entry.Time.UnixNano(), 10))
	message = strings.ReplaceAll(message, "%unix%", strconv.FormatInt(entry.Time.Unix(), 10))
	message = strings.ReplaceAll(message, "%time%", entry.Time.Format("2006-01-02 15:04:05.000"))
	message = strings.ReplaceAll(message, "%name%", inst.Name)
	message = strings.ReplaceAll(message, "%flag%", inst.Config.Flag)
	message = strings.ReplaceAll(message, "%level%", levelStrings[entry.Level])
	message = strings.ReplaceAll(message, "%body%", entry.Body)
	message = strings.ReplaceAll(message, "%project%", fieldString(entry.Fields, "project"))
	message = strings.ReplaceAll(message, "%profile%", fieldString(entry.Fields, "profile"))
	message = strings.ReplaceAll(message, "%node%", fieldString(entry.Fields, "node"))
	if len(entry.Fields) > 0 {
		message += " " + formatFields(entry.Fields)
	}

	return message
}

func (inst *Instance) Allow(level Level, body string, fields Map) bool {
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
	return hash01(level, inst.Name, body, fields) <= r
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

func hash01(level Level, name, body string, fields Map) float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strconv.Itoa(level)))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(name))
	_, _ = h.Write([]byte(":"))
	_, _ = h.Write([]byte(body))
	if len(fields) > 0 {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = h.Write([]byte("|"))
			_, _ = h.Write([]byte(k))
			_, _ = h.Write([]byte("="))
			_, _ = h.Write([]byte(fmt.Sprintf("%v", fields[k])))
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
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
	}
	return strings.Join(parts, " ")
}

func fieldString(fields Map, key string) string {
	if fields == nil {
		return ""
	}
	if v, ok := fields[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
