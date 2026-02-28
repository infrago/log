package log

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	writers:   make(map[string]*instanceWriter, 0),
}

type (
	overflowMode int

	instanceWriter struct {
		instance *Instance
		mode     overflowMode
		queue    chan []Log
		stopChan chan struct{}
		doneChan chan struct{}

		queuedCount      atomic.Int64
		dropCount        atomic.Int64
		writeErrorCount  atomic.Int64
		lastWriteLatency atomic.Int64
		totalWriteNs     atomic.Int64
		writeCount       atomic.Int64
	}

	Module struct {
		mutex sync.RWMutex

		opened  bool
		started bool

		configs   map[string]Config
		drivers   map[string]Driver
		instances map[string]*Instance
		writers   map[string]*instanceWriter

		queue    chan Log
		stopChan chan struct{}
		doneChan chan struct{}

		overflowMode overflowMode
		batchSize    int
		dropped      uint64

		queuedCount           atomic.Int64
		syncFallbackCount     atomic.Int64
		dropCount             atomic.Int64
		flushCount            atomic.Int64
		flushLogCount         atomic.Int64
		writeErrorCount       atomic.Int64
		lastFlushLatencyMs    atomic.Int64
		lastDispatchLatencyMs atomic.Int64
		totalDispatchNs       atomic.Int64
	}

	Configs map[string]Config
	Config  struct {
		Driver   string
		Level    Level
		Levels   map[Level]bool
		Sample   float64
		Json     bool
		Buffer   int
		Batch    int
		Timeout  time.Duration
		Block    bool
		Overflow string
		Drop     string
		Flag     string
		Format   string
		Setting  Map
	}

	Log struct {
		Time   time.Time
		Level  Level
		Body   string
		Fields Map
	}
)

const (
	overflowDropNewest overflowMode = iota
	overflowDropOldest
	overflowBlock
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
	cfg := Config{Driver: bamgoo.DEFAULT, Level: LevelDebug, Sample: 1}
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
	if v, ok := parseFloat(conf["sample"]); ok {
		cfg.Sample = v
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
	if v, ok := parseInt(conf["batch"]); ok && v > 0 {
		cfg.Batch = v
	}
	if v, ok := parseDuration(conf["timeout"]); ok && v > 0 {
		cfg.Timeout = v
	}
	if v, ok := parseBool(conf["block"]); ok {
		cfg.Block = v
	}
	if v, ok := conf["overflow"].(string); ok {
		cfg.Overflow = strings.ToLower(strings.TrimSpace(v))
	}
	if v, ok := conf["drop"].(string); ok {
		cfg.Drop = strings.ToLower(strings.TrimSpace(v))
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
	batchSize := 512
	mode := overflowDropNewest
	for _, inst := range m.instances {
		if inst.Config.Buffer > bufferSize {
			bufferSize = inst.Config.Buffer
		}
		if inst.Config.Batch > batchSize {
			batchSize = inst.Config.Batch
		}
		if inst.Config.Timeout > 0 && inst.Config.Timeout < flushEvery {
			flushEvery = inst.Config.Timeout
		}
		switch parseOverflow(inst.Config) {
		case overflowBlock:
			mode = overflowBlock
		case overflowDropOldest:
			if mode != overflowBlock {
				mode = overflowDropOldest
			}
		}
	}

	m.queue = make(chan Log, bufferSize)
	m.stopChan = make(chan struct{})
	m.doneChan = make(chan struct{})
	m.writers = make(map[string]*instanceWriter, len(m.instances))
	for name, inst := range m.instances {
		workerQueueSize := inst.Config.Buffer
		if workerQueueSize <= 0 {
			workerQueueSize = 256
		}
		worker := newInstanceWriter(inst, mode, workerQueueSize)
		m.writers[name] = worker
		worker.start()
	}
	m.overflowMode = mode
	m.batchSize = batchSize
	atomic.StoreUint64(&m.dropped, 0)
	m.queuedCount.Store(0)
	m.syncFallbackCount.Store(0)
	m.dropCount.Store(0)
	m.flushCount.Store(0)
	m.flushLogCount.Store(0)
	m.writeErrorCount.Store(0)
	m.lastFlushLatencyMs.Store(0)
	m.lastDispatchLatencyMs.Store(0)
	m.totalDispatchNs.Store(0)
	go m.loop(flushEvery, batchSize)

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

	m.mutex.Lock()
	writers := make([]*instanceWriter, 0, len(m.writers))
	for _, w := range m.writers {
		writers = append(writers, w)
	}
	m.mutex.Unlock()
	for _, w := range writers {
		w.stop()
	}
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
	m.writers = make(map[string]*instanceWriter, 0)
	m.opened = false
}

func (m *Module) loop(flushEvery time.Duration, batchSize int) {
	defer close(m.doneChan)

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	batch := make([]Log, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		start := time.Now()
		dropped := m.dispatch(batch)
		elapsed := time.Since(start)
		latencyMs := elapsed.Milliseconds()
		m.flushCount.Add(1)
		m.flushLogCount.Add(int64(len(batch)))
		if dropped > 0 {
			m.dropCount.Add(int64(dropped))
			atomic.AddUint64(&m.dropped, uint64(dropped))
		}
		m.lastFlushLatencyMs.Store(latencyMs)
		m.lastDispatchLatencyMs.Store(latencyMs)
		m.totalDispatchNs.Add(elapsed.Nanoseconds())
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
			m.reportDropped()
		case <-m.stopChan:
			for {
				select {
				case entry := <-m.queue:
					batch = append(batch, entry)
				default:
					flush()
					m.reportDropped()
					return
				}
			}
		}
	}
}

func (m *Module) reportDropped() {
	n := atomic.SwapUint64(&m.dropped, 0)
	if n == 0 {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "bamgoo log queue is full, dropped %d entries\n", n)
}

func (m *Module) dispatch(entries []Log) int {
	m.mutex.RLock()
	instances := make([]*Instance, 0, len(m.instances))
	writers := make(map[string]*instanceWriter, len(m.writers))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	for name, writer := range m.writers {
		writers[name] = writer
	}
	m.mutex.RUnlock()

	dropped := 0
	for _, inst := range instances {
		filtered := make([]Log, 0, len(entries))
		for _, entry := range entries {
			if inst.Allow(entry.Level, entry.Body, entry.Fields) {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		writer := writers[inst.Name]
		if writer == nil {
			if err := inst.connect.Write(filtered...); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
				m.writeErrorCount.Add(1)
			}
			continue
		}
		if n := writer.enqueue(filtered); n > 0 {
			dropped += n
		}
	}
	return dropped
}

func (m *Module) Write(entry Log) {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	entry.Fields = ensureIdentityFields(entry.Fields)

	m.mutex.RLock()
	started := m.started
	queue := m.queue
	mode := m.overflowMode
	m.mutex.RUnlock()

	if started && queue != nil {
		if mode == overflowBlock {
			queue <- entry
			return
		}
		select {
		case queue <- entry:
			m.queuedCount.Add(1)
			return
		default:
			switch mode {
			case overflowDropOldest:
				select {
				case <-queue:
					m.dropCount.Add(1)
					atomic.AddUint64(&m.dropped, 1)
				default:
				}
				select {
				case queue <- entry:
					m.queuedCount.Add(1)
					return
				default:
					m.dropCount.Add(1)
					atomic.AddUint64(&m.dropped, 1)
					return
				}
			default:
				m.dropCount.Add(1)
				atomic.AddUint64(&m.dropped, 1)
			}
			return
		}
	}
	m.syncFallbackCount.Add(1)
	start := time.Now()
	writeErrors := 0
	m.mutex.RLock()
	instances := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	m.mutex.RUnlock()
	for _, inst := range instances {
		if !inst.Allow(entry.Level, entry.Body, entry.Fields) {
			continue
		}
		if err := inst.connect.Write(entry); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
			writeErrors++
		}
	}
	elapsed := time.Since(start)
	m.flushCount.Add(1)
	m.flushLogCount.Add(1)
	m.writeErrorCount.Add(int64(writeErrors))
	m.lastFlushLatencyMs.Store(elapsed.Milliseconds())
	m.lastDispatchLatencyMs.Store(elapsed.Milliseconds())
	m.totalDispatchNs.Add(elapsed.Nanoseconds())
}

func ensureIdentityFields(fields Map) Map {
	if fields == nil {
		fields = Map{}
	}
	identity := bamgoo.Identity()
	if _, ok := fields["project"]; !ok {
		fields["project"] = identity.Project
	}
	if _, ok := fields["profile"]; !ok {
		fields["profile"] = identity.Profile
	}
	if _, ok := fields["node"]; !ok {
		fields["node"] = identity.Node
	}
	return fields
}

func (m *Module) Stats() Map {
	m.mutex.RLock()
	queueLen := 0
	queueCap := 0
	started := m.started
	if m.queue != nil {
		queueLen = len(m.queue)
		queueCap = cap(m.queue)
	}
	connectionsCount := len(m.instances)
	overflow := overflowModeName(m.overflowMode)
	batch := m.batchSize
	instanceQueues := Map{}
	for name, writer := range m.writers {
		instanceQueues[name] = writer.stats()
	}
	m.mutex.RUnlock()

	flushCount := m.flushCount.Load()
	avgDispatchMs := int64(0)
	if flushCount > 0 {
		avgDispatchMs = (m.totalDispatchNs.Load() / flushCount) / int64(time.Millisecond)
	}

	return Map{
		"started":                  started,
		"connections_count":        connectionsCount,
		"queue_len":                queueLen,
		"queue_cap":                queueCap,
		"overflow":                 overflow,
		"batch":                    batch,
		"queued_count":             m.queuedCount.Load(),
		"sync_fallback_count":      m.syncFallbackCount.Load(),
		"drop_count":               m.dropCount.Load(),
		"flush_count":              flushCount,
		"flush_log_count":          m.flushLogCount.Load(),
		"write_error_count":        m.writeErrorCount.Load(),
		"last_flush_latency_ms":    m.lastFlushLatencyMs.Load(),
		"last_dispatch_latency_ms": m.lastDispatchLatencyMs.Load(),
		"avg_dispatch_latency_ms":  avgDispatchMs,
		"connections":              instanceQueues,
	}
}

func overflowModeName(mode overflowMode) string {
	switch mode {
	case overflowBlock:
		return OverflowBlock
	case overflowDropOldest:
		return OverflowDrop + ":" + DropOld
	case overflowDropNewest:
		return OverflowDrop + ":" + DropNew
	default:
		return OverflowBlock
	}
}

func newInstanceWriter(inst *Instance, mode overflowMode, queueSize int) *instanceWriter {
	if queueSize <= 0 {
		queueSize = 256
	}
	return &instanceWriter{
		instance: inst,
		mode:     mode,
		queue:    make(chan []Log, queueSize),
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func (w *instanceWriter) start() {
	go func() {
		defer close(w.doneChan)
		for {
			select {
			case entries := <-w.queue:
				if len(entries) == 0 {
					continue
				}
				start := time.Now()
				if err := w.instance.connect.Write(entries...); err != nil {
					_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
					w.writeErrorCount.Add(1)
					module.writeErrorCount.Add(1)
				}
				elapsed := time.Since(start)
				w.lastWriteLatency.Store(elapsed.Milliseconds())
				w.totalWriteNs.Add(elapsed.Nanoseconds())
				w.writeCount.Add(1)
			case <-w.stopChan:
				for {
					select {
					case entries := <-w.queue:
						if len(entries) == 0 {
							continue
						}
						start := time.Now()
						if err := w.instance.connect.Write(entries...); err != nil {
							_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
							w.writeErrorCount.Add(1)
							module.writeErrorCount.Add(1)
						}
						elapsed := time.Since(start)
						w.lastWriteLatency.Store(elapsed.Milliseconds())
						w.totalWriteNs.Add(elapsed.Nanoseconds())
						w.writeCount.Add(1)
					default:
						return
					}
				}
			}
		}
	}()
}

func (w *instanceWriter) stop() {
	close(w.stopChan)
	<-w.doneChan
}

func (w *instanceWriter) enqueue(entries []Log) int {
	if len(entries) == 0 {
		return 0
	}
	if w.mode == overflowBlock {
		w.queue <- entries
		w.queuedCount.Add(int64(len(entries)))
		return 0
	}

	select {
	case w.queue <- entries:
		w.queuedCount.Add(int64(len(entries)))
		return 0
	default:
		if w.mode == overflowDropOldest {
			select {
			case old := <-w.queue:
				dropped := len(old)
				w.dropCount.Add(int64(dropped))
				select {
				case w.queue <- entries:
					w.queuedCount.Add(int64(len(entries)))
					return dropped
				default:
					w.dropCount.Add(int64(len(entries)))
					return dropped + len(entries)
				}
			default:
			}
		}
		w.dropCount.Add(int64(len(entries)))
		return len(entries)
	}
}

func (w *instanceWriter) stats() Map {
	queueLen := len(w.queue)
	queueCap := cap(w.queue)
	writeCount := w.writeCount.Load()
	avgLatencyMs := int64(0)
	if writeCount > 0 {
		avgLatencyMs = (w.totalWriteNs.Load() / writeCount) / int64(time.Millisecond)
	}
	return Map{
		"queue_len":             queueLen,
		"queue_cap":             queueCap,
		"queued_count":          w.queuedCount.Load(),
		"drop_count":            w.dropCount.Load(),
		"write_error_count":     w.writeErrorCount.Load(),
		"last_write_latency_ms": w.lastWriteLatency.Load(),
		"avg_write_latency_ms":  avgLatencyMs,
	}
}

func parseOverflow(cfg Config) overflowMode {
	if cfg.Block {
		return overflowBlock
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Overflow)) {
	case OverflowBlock:
		return overflowBlock
	case OverflowDrop:
		switch strings.ToLower(strings.TrimSpace(cfg.Drop)) {
		case DropNew:
			return overflowDropNewest
		default:
			return overflowDropOldest
		}
	case OverflowDropOldest:
		return overflowDropOldest
	case OverflowDropNewest:
		return overflowDropNewest
	default:
		return overflowBlock
	}
}

func (m *Module) Logging(level Level, args ...Any) {
	body, fields := m.parseArgs(args...)
	m.Write(Log{
		Time:   time.Now(),
		Level:  level,
		Body:   body,
		Fields: fields,
	})
}

func (m *Module) Loggingf(level Level, format string, args ...Any) {
	m.Write(Log{
		Time:  time.Now(),
		Level: level,
		Body:  fmt.Sprintf(format, args...),
	})
}

func (m *Module) Loggingw(level Level, body string, fields Map) {
	m.Write(Log{
		Time:   time.Now(),
		Level:  level,
		Body:   body,
		Fields: cloneMap(fields),
	})
}

func (m *Module) parseArgs(args ...Any) (string, Map) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 1 {
		if m, ok := args[0].(Map); ok {
			return "", cloneMap(m)
		}
		return fmt.Sprintf("%v", args[0]), nil
	}

	if last, ok := args[len(args)-1].(Map); ok {
		return m.parseBody(args[:len(args)-1]...), cloneMap(last)
	}

	return m.parseBody(args...), nil
}

func (m *Module) parseBody(args ...Any) string {
	if len(args) == 0 {
		return ""
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
		upper := strings.ToUpper(strings.TrimSpace(v))
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
	case string:
		parts := strings.Split(v, ",")
		for _, item := range parts {
			if level, ok := parseLevel(strings.TrimSpace(item)); ok {
				levels[level] = true
			}
		}
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
	case Map:
		for key, raw := range v {
			if allow, ok := parseBool(raw); ok && allow {
				if level, ok := parseLevel(key); ok {
					levels[level] = true
				}
			}
		}
	}

	if len(levels) == 0 {
		return nil, false
	}
	return levels, true
}

func parseBool(value Any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case int:
		return v != 0, true
	case int64:
		return v != 0, true
	case float64:
		return v != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	}
	return false, false
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

func parseFloat(value Any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func castMap(value Any) (Map, bool) {
	v, ok := value.(Map)
	return v, ok
}

func cloneMap(src Map) Map {
	if src == nil {
		return nil
	}
	out := make(Map, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
