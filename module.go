package log

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bamgoo/bamgoo"
	. "github.com/bamgoo/base"
)

func init() {
	bamgoo.Mount(module)
}

var module = &Module{
	configs:   make(map[string]Config, 0),
	drivers:   make(map[string]Driver, 0),
	instances: make(map[string]*Instance, 0),
}

type (
	Module struct {
		mutex sync.RWMutex

		opened  bool
		started bool

		configs   map[string]Config
		drivers   map[string]Driver
		instances map[string]*Instance

		queue    chan Log
		stopChan chan struct{}
		doneChan chan struct{}
	}

	Configs map[string]Config
	Config  struct {
		Driver  string
		Level   Level
		Levels  map[Level]bool
		Json    bool
		Buffer  int
		Timeout time.Duration
		Flag    string
		Format  string
		Setting Map
	}

	Log struct {
		Time  time.Time
		Level Level
		Body  string
	}
)

func (m *Module) Register(name string, value Any) {
	switch v := value.(type) {
	case Driver:
		m.RegisterDriver(name, v)
	case Config:
		m.RegisterConfig(name, v)
	case Configs:
		m.RegisterConfigs(v)
	}
}

func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if name == "" {
		name = bamgoo.DEFAULT
	}
	if driver == nil {
		panic(errInvalidLogDriver)
	}
	if bamgoo.Override() {
		m.drivers[name] = driver
	} else {
		if _, ok := m.drivers[name]; !ok {
			m.drivers[name] = driver
		}
	}
}

func (m *Module) RegisterConfig(name string, cfg Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	if name == "" {
		name = bamgoo.DEFAULT
	}
	if bamgoo.Override() {
		m.configs[name] = cfg
	} else {
		if _, ok := m.configs[name]; !ok {
			m.configs[name] = cfg
		}
	}
}

func (m *Module) RegisterConfigs(configs Configs) {
	for name, cfg := range configs {
		m.RegisterConfig(name, cfg)
	}
}

func (m *Module) Config(global Map) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	cfgAny, ok := global["log"]
	if !ok {
		return
	}
	cfgMap, ok := castMap(cfgAny)
	if !ok || cfgMap == nil {
		return
	}

	rootConfig := Map{}
	for key, val := range cfgMap {
		if conf, ok := castMap(val); ok && key != "setting" {
			m.configure(key, conf)
		} else {
			rootConfig[key] = val
		}
	}
	if len(rootConfig) > 0 {
		m.configure(bamgoo.DEFAULT, rootConfig)
	}
}

func (m *Module) configure(name string, conf Map) {
	cfg := Config{Driver: bamgoo.DEFAULT, Level: LevelDebug}
	if existing, ok := m.configs[name]; ok {
		cfg = existing
	}

	if v, ok := conf["driver"].(string); ok && v != "" {
		cfg.Driver = v
	}
	if v, ok := parseLevel(conf["level"]); ok {
		cfg.Level = v
	}
	if levels, ok := parseLevels(conf["levels"]); ok {
		cfg.Levels = levels
	}
	if v, ok := conf["json"].(bool); ok {
		cfg.Json = v
	}
	if v, ok := conf["flag"].(string); ok {
		cfg.Flag = v
	}
	if v, ok := conf["format"].(string); ok {
		cfg.Format = v
	}
	if v, ok := parseInt(conf["buffer"]); ok && v > 0 {
		cfg.Buffer = v
	}
	if v, ok := parseDuration(conf["timeout"]); ok && v > 0 {
		cfg.Timeout = v
	}
	if v, ok := castMap(conf["setting"]); ok {
		cfg.Setting = v
	}

	m.configs[name] = cfg
}

func (m *Module) Setup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	if len(m.configs) == 0 {
		m.configs[bamgoo.DEFAULT] = normalizeConfig(Config{
			Driver: bamgoo.DEFAULT,
			Level:  LevelDebug,
		})
		return
	}

	for name, cfg := range m.configs {
		m.configs[name] = normalizeConfig(cfg)
	}
}

func (m *Module) Open() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.opened {
		return
	}

	for name, cfg := range m.configs {
		driver := m.drivers[cfg.Driver]
		if driver == nil {
			panic("invalid log driver: " + cfg.Driver)
		}

		inst := &Instance{
			Name:    name,
			Config:  cfg,
			Setting: cfg.Setting,
		}

		conn, err := driver.Connect(inst)
		if err != nil {
			panic("failed to connect log: " + err.Error())
		}
		if err := conn.Open(); err != nil {
			panic("failed to open log: " + err.Error())
		}
		inst.connect = conn
		m.instances[name] = inst
	}

	m.opened = true
}

func (m *Module) Start() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.started {
		return
	}

	bufferSize := 1024
	flushEvery := time.Millisecond * 200
	for _, inst := range m.instances {
		if inst.Config.Buffer > bufferSize {
			bufferSize = inst.Config.Buffer
		}
		if inst.Config.Timeout > 0 && inst.Config.Timeout < flushEvery {
			flushEvery = inst.Config.Timeout
		}
	}

	m.queue = make(chan Log, bufferSize)
	m.stopChan = make(chan struct{})
	m.doneChan = make(chan struct{})
	go m.loop(flushEvery)

	m.started = true
	fmt.Printf("bamgoo log module is running with %d connections.\n", len(m.instances))
}

func (m *Module) Stop() {
	m.mutex.Lock()
	if !m.started {
		m.mutex.Unlock()
		return
	}
	stopCh := m.stopChan
	doneCh := m.doneChan
	m.started = false
	m.mutex.Unlock()

	close(stopCh)
	<-doneCh
}

func (m *Module) Close() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.opened {
		return
	}

	for _, inst := range m.instances {
		_ = inst.connect.Close()
	}

	m.instances = make(map[string]*Instance, 0)
	m.opened = false
}

func (m *Module) loop(flushEvery time.Duration) {
	defer close(m.doneChan)

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	batch := make([]Log, 0, 256)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		m.dispatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-m.queue:
			batch = append(batch, entry)
			if len(batch) >= cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-m.stopChan:
			for {
				select {
				case entry := <-m.queue:
					batch = append(batch, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (m *Module) dispatch(entries []Log) {
	m.mutex.RLock()
	instances := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	m.mutex.RUnlock()

	for _, inst := range instances {
		filtered := make([]Log, 0, len(entries))
		for _, entry := range entries {
			if inst.Allow(entry.Level) {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		if err := inst.connect.Write(filtered...); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
		}
	}
}

func (m *Module) Write(entry Log) {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}

	m.mutex.RLock()
	started := m.started
	queue := m.queue
	m.mutex.RUnlock()

	if started && queue != nil {
		select {
		case queue <- entry:
			return
		default:
			// Back-pressure fallback: do a direct write if queue is full.
		}
	}
	m.dispatch([]Log{entry})
}

func (m *Module) Logging(level Level, args ...Any) {
	m.Write(Log{
		Time:  time.Now(),
		Level: level,
		Body:  m.parseBody(args...),
	})
}

func (m *Module) parseBody(args ...Any) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return fmt.Sprintf("%v", args[0])
	}

	if format, ok := args[0].(string); ok {
		verbCount := strings.Count(format, "%") - strings.Count(format, "%%")
		if verbCount > 0 && verbCount == (len(args)-1) {
			return fmt.Sprintf(format, args[1:]...)
		}
	}

	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%v", arg))
	}
	return strings.Join(parts, " ")
}

func parseLevel(value Any) (Level, bool) {
	switch v := value.(type) {
	case int:
		return Level(v), true
	case int64:
		return Level(v), true
	case float64:
		return Level(v), true
	case string:
		upper := strings.ToUpper(v)
		for level, name := range levelStrings {
			if upper == name {
				return level, true
			}
		}
	}
	return LevelDebug, false
}

func parseLevels(value Any) (map[Level]bool, bool) {
	levels := map[Level]bool{}

	switch v := value.(type) {
	case []string:
		for _, item := range v {
			if level, ok := parseLevel(item); ok {
				levels[level] = true
			}
		}
	case []Any:
		for _, item := range v {
			if level, ok := parseLevel(item); ok {
				levels[level] = true
			}
		}
	}

	if len(levels) == 0 {
		return nil, false
	}
	return levels, true
}

func parseInt(value Any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func parseDuration(value Any) (time.Duration, bool) {
	switch v := value.(type) {
	case time.Duration:
		return v, true
	case int:
		return time.Second * time.Duration(v), true
	case int64:
		return time.Second * time.Duration(v), true
	case float64:
		return time.Second * time.Duration(v), true
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d, true
		}
	}
	return 0, false
}

func castMap(value Any) (Map, bool) {
	v, ok := value.(Map)
	return v, ok
}
