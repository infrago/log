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
	writeMu sync.RWMutex

	Name    string
	Config  Config
	Setting map[string]any

	formatOnce  sync.Once
	formatParts []formatPart

	sampleMillion uint64
	allLevels     bool
	needsSample   bool
	customLevels  bool
	levelMask     uint64
	allowedLevels []Level
}

const logTimeFormat = "2006-01-02 15:04:05.000000"

func (inst *Instance) Format(entry Log) string {
	if inst.Config.Json {
		return inst.formatJSON(entry)
	}

	message := inst.formatText(entry)
	if len(entry.Fields) > 0 {
		message += " " + formatFields(entry.Fields)
	}

	return message
}

func (inst *Instance) formatJSON(entry Log) string {
	var fields []byte
	if len(entry.Fields) > 0 {
		var err error
		fields, err = json.Marshal(entry.Fields)
		if err != nil {
			fields, err = json.Marshal(stringifyFields(entry.Fields, err))
			if err != nil {
				return inst.formatText(entry) + " " + formatFields(entry.Fields)
			}
		}
	}

	var b strings.Builder
	b.Grow(len(entry.Body) + len(entry.Project) + len(entry.Role) + len(entry.Profile) + len(entry.Node) + len(fields) + 160)
	b.WriteByte('{')
	writeJSONField(&b, "time", entry.Time.Format(logTimeFormat), false)
	writeJSONIntField(&b, "unix", entry.Time.Unix())
	writeJSONIntField(&b, "nano", entry.Time.UnixNano())
	writeJSONField(&b, "level", levelStrings[entry.Level], true)
	writeJSONField(&b, "flag", inst.Config.Flag, true)
	writeJSONField(&b, "body", entry.Body, true)
	writeJSONField(&b, "project", entry.Project, true)
	writeJSONField(&b, "role", entry.Role, true)
	writeJSONField(&b, "profile", entry.Profile, true)
	writeJSONField(&b, "node", entry.Node, true)
	if len(fields) > 0 {
		b.WriteString(`,"fields":`)
		b.Write(fields)
	}
	b.WriteByte('}')
	return b.String()
}

func writeJSONField(b *strings.Builder, key, value string, comma bool) {
	if comma {
		b.WriteByte(',')
	}
	b.WriteByte('"')
	b.WriteString(key)
	b.WriteString(`":`)
	b.WriteString(strconv.Quote(value))
}

func writeJSONIntField(b *strings.Builder, key string, value int64) {
	b.WriteByte(',')
	b.WriteByte('"')
	b.WriteString(key)
	b.WriteString(`":`)
	b.WriteString(strconv.FormatInt(value, 10))
}

func (inst *Instance) Allow(level Level, body, project, role, profile, node string, fields Map) bool {
	if !inst.Config.Levels[level] {
		return false
	}
	if inst.sampleMillion >= sampleScale {
		return true
	}
	if inst.sampleMillion == 0 {
		return false
	}
	return hashMillion(level, inst.Name, body, project, role, profile, node, fields) <= inst.sampleMillion
}

func (inst *Instance) prepare() {
	switch {
	case inst.Config.Sample <= 0:
		inst.sampleMillion = 0
	case inst.Config.Sample >= 1:
		inst.sampleMillion = sampleScale
	default:
		inst.sampleMillion = uint64(inst.Config.Sample * float64(sampleScale))
	}
	inst.needsSample = inst.sampleMillion > 0 && inst.sampleMillion < sampleScale
	inst.allLevels = true
	inst.customLevels = false
	inst.levelMask = 0
	inst.allowedLevels = inst.allowedLevels[:0]
	for level := range levelStrings {
		if inst.Config.Levels[level] {
			inst.allowedLevels = append(inst.allowedLevels, level)
			if bit, ok := levelBit(level); ok {
				inst.levelMask |= bit
			}
		} else {
			inst.allLevels = false
		}
	}
	for level, allowed := range inst.Config.Levels {
		if allowed && (level < LevelFatal || level > LevelDebug) {
			inst.customLevels = true
			inst.allLevels = false
			break
		}
	}
}

func (inst *Instance) allowAll() bool {
	return inst.allLevels && inst.sampleMillion >= sampleScale
}

func (inst *Instance) allowLevelOnly(level Level) bool {
	return inst.Config.Levels[level]
}

func levelBit(level Level) (uint64, bool) {
	if level < 0 || level >= 64 {
		return 0, false
	}
	return uint64(1) << uint(level), true
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
	if cfg.Sample < 0 {
		cfg.Sample = 0
	} else if cfg.Sample == 0 {
		cfg.Sample = 1
	}
	if cfg.Sample > 1 {
		cfg.Sample = 1
	}
	cfg = normalizeLevels(cfg)
	return cfg
}

const sampleScale uint64 = 1000000

var fieldKeysPool = sync.Pool{
	New: func() any {
		keys := make([]string, 0, 16)
		return &keys
	},
}

func hashMillion(level Level, name, body, project, role, profile, node string, fields Map) uint64 {
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
		keysPtr, keys := sortedFieldKeys(fields)
		for _, k := range keys {
			hashWrite(h, "|")
			hashWrite(h, k)
			hashWrite(h, "=")
			hashWrite(h, fmt.Sprint(fields[k]))
		}
		putFieldKeys(keysPtr, keys)
	}
	return h.Sum64() % sampleScale
}

func formatFields(fields Map) string {
	keysPtr, keys := sortedFieldKeys(fields)
	defer putFieldKeys(keysPtr, keys)
	var b strings.Builder
	b.Grow(len(fields) * 16)
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

func sortedFieldKeys(fields Map) (*[]string, []string) {
	keysPtr := fieldKeysPool.Get().(*[]string)
	keys := (*keysPtr)[:0]
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keysPtr, keys
}

func putFieldKeys(keysPtr *[]string, keys []string) {
	if cap(keys) > 1024 {
		return
	}
	for i := range keys {
		keys[i] = ""
	}
	*keysPtr = keys[:0]
	fieldKeysPool.Put(keysPtr)
}

func stringifyFields(fields Map, encodeErr error) Map {
	out := make(Map, len(fields)+1)
	if encodeErr != nil {
		out["_error"] = encodeErr.Error()
	}
	for key, value := range fields {
		if _, err := json.Marshal(value); err == nil {
			out[key] = value
		} else {
			out[key] = fmt.Sprint(value)
		}
	}
	return out
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
