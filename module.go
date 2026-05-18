package log

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func init() {
	infra.Mount(module)
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
		module   *Module
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
		lastError        atomic.Value
	}

	instanceDispatcher struct {
		instance *Instance
		writer   *instanceWriter
	}

	Module struct {
		mutex    sync.RWMutex
		logsPool sync.Pool

		opened  bool
		started bool

		configs      map[string]Config
		drivers      map[string]Driver
		instances    map[string]*Instance
		instanceList []*Instance
		writers      map[string]*instanceWriter
		dispatchers  []instanceDispatcher

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
		lastDropUnix          atomic.Int64
		lastError             atomic.Value
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
		Time    time.Time
		Level   Level
		Body    string
		Project string
		Role    string
		Profile string
		Node    string
		Fields  Map
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
		name = infra.DEFAULT
	}
	if driver == nil {
		panic(errInvalidLogDriver)
	}
	if infra.Override() {
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
		name = infra.DEFAULT
	}
	if infra.Override() {
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
		m.configure(infra.DEFAULT, rootConfig)
	}
}

func (m *Module) configure(name string, conf Map) {
	cfg := Config{Driver: infra.DEFAULT, Level: LevelDebug, Sample: 1}
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
		if v <= 0 {
			cfg.Sample = -1
		} else {
			cfg.Sample = v
		}
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
		m.configs[infra.DEFAULT] = normalizeConfig(Config{
			Driver: infra.DEFAULT,
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

	opened := make([]Connection, 0, len(m.configs))
	rollback := func() {
		for _, conn := range opened {
			_ = conn.Close()
		}
		m.instances = make(map[string]*Instance, 0)
		m.instanceList = nil
	}
	for name, cfg := range m.configs {
		driver := m.drivers[cfg.Driver]
		if driver == nil {
			rollback()
			panic("invalid log driver: " + cfg.Driver)
		}

		inst := &Instance{
			Name:    name,
			Config:  cfg,
			Setting: cfg.Setting,
		}
		inst.prepare()

		conn, err := driver.Connect(inst)
		if err != nil {
			rollback()
			panic("failed to connect log " + name + ": " + err.Error())
		}
		if err := conn.Open(); err != nil {
			rollback()
			_ = conn.Close()
			panic("failed to open log " + name + ": " + err.Error())
		}
		inst.connect = conn
		m.instances[name] = inst
		m.instanceList = append(m.instanceList, inst)
		opened = append(opened, conn)
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
	for _, inst := range m.instanceList {
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
		worker := newInstanceWriter(m, inst, parseOverflow(inst.Config), workerQueueSize)
		m.writers[name] = worker
		worker.start()
	}
	m.dispatchers = make([]instanceDispatcher, 0, len(m.instances))
	for _, inst := range m.instanceList {
		m.dispatchers = append(m.dispatchers, instanceDispatcher{
			instance: inst,
			writer:   m.writers[inst.Name],
		})
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
	m.lastDropUnix.Store(0)
	m.lastError.Store("")
	go m.loop(flushEvery, batchSize)

	m.started = true
	fmt.Printf("infrago log module is running with %d connections.\n", len(m.instances))
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
	m.Stop()

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.opened {
		return
	}

	for _, inst := range m.instances {
		inst.writeMu.Lock()
		_ = inst.connect.Close()
		inst.writeMu.Unlock()
	}

	m.instances = make(map[string]*Instance, 0)
	m.instanceList = nil
	m.writers = make(map[string]*instanceWriter, 0)
	m.dispatchers = nil
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
			m.recordDrop(dropped)
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
	_, _ = fmt.Fprintf(os.Stderr, "infrago log queue is full, dropped %d entries\n", n)
}

func (m *Module) recordDrop(n int) {
	if n <= 0 {
		return
	}
	m.dropCount.Add(int64(n))
	m.markDropTime()
	atomic.AddUint64(&m.dropped, uint64(n))
}

func (m *Module) markDropTime() {
	now := time.Now().Unix()
	if m.lastDropUnix.Load() != now {
		m.lastDropUnix.Store(now)
	}
}

func (m *Module) recordWriteError(err error) {
	if err == nil {
		return
	}
	m.writeErrorCount.Add(1)
	m.lastError.Store(err.Error())
}

func (m *Module) dispatch(entries []Log) int {
	m.mutex.RLock()
	dispatchers := m.dispatchers
	m.mutex.RUnlock()

	dropped := 0
	for _, dispatcher := range dispatchers {
		inst := dispatcher.instance
		if inst.allowAll() {
			if n := m.dispatchEntries(dispatcher, entries, false); n > 0 {
				dropped += n
			}
			continue
		}

		filtered := m.getLogSlice(len(entries))
		if inst.needsSample {
			for _, entry := range entries {
				if inst.Allow(entry.Level, entry.Body, entry.Project, entry.Role, entry.Profile, entry.Node, entry.Fields) {
					filtered = append(filtered, entry)
				}
			}
		} else {
			for _, entry := range entries {
				if inst.allowLevelOnly(entry.Level) {
					filtered = append(filtered, entry)
				}
			}
		}
		if n := m.dispatchEntries(dispatcher, filtered, true); n > 0 {
			dropped += n
		}
	}
	return dropped
}

func (m *Module) dispatchEntries(dispatcher instanceDispatcher, entries []Log, pooled bool) int {
	if len(entries) == 0 {
		if pooled {
			m.putLogSlice(entries)
		}
		return 0
	}

	inst := dispatcher.instance
	writer := dispatcher.writer
	if writer == nil {
		inst.writeMu.RLock()
		if err := inst.connect.Write(entries...); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
			m.recordWriteError(err)
		}
		inst.writeMu.RUnlock()
		if pooled {
			m.putLogSlice(entries)
		}
		return 0
	}

	if !pooled {
		cloned := m.getLogSlice(len(entries))
		cloned = append(cloned, entries...)
		entries = cloned
	}
	return writer.enqueue(entries)
}

func (m *Module) getLogSlice(size int) []Log {
	if value := m.logsPool.Get(); value != nil {
		logs := value.([]Log)
		if cap(logs) >= size {
			return logs[:0]
		}
	}
	return make([]Log, 0, size)
}

func (m *Module) putLogSlice(logs []Log) {
	if logs == nil {
		return
	}
	if cap(logs) > 65536 {
		return
	}
	for i := range logs {
		logs[i] = Log{}
	}
	m.logsPool.Put(logs[:0])
}

func (m *Module) Write(entry Log) {
	entry = m.normalizeEntry(entry)

	if m.enqueue(entry) {
		return
	}

	m.writeSyncEntry(entry, true)
}

func (m *Module) enqueue(entry Log) bool {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		m.mutex.RLock()
		if !m.started || m.queue == nil {
			m.mutex.RUnlock()
			return false
		}
		queue := m.queue
		stopCh := m.stopChan
		mode := m.overflowMode
		select {
		case queue <- entry:
			m.queuedCount.Add(1)
			m.mutex.RUnlock()
			return true
		default:
		}
		if mode == overflowBlock {
			m.mutex.RUnlock()
			if timer == nil {
				timer = time.NewTimer(time.Millisecond)
			} else {
				timer.Reset(time.Millisecond)
			}
			select {
			case <-stopCh:
				return false
			case <-timer.C:
				continue
			}
		}
		if mode == overflowDropOldest {
			select {
			case <-queue:
				m.recordDrop(1)
			default:
			}
			select {
			case queue <- entry:
				m.queuedCount.Add(1)
				m.mutex.RUnlock()
				return true
			default:
				m.recordDrop(1)
				m.mutex.RUnlock()
				return true
			}
		}
		m.recordDrop(1)
		m.mutex.RUnlock()
		return true
	}
}

func (m *Module) WriteSync(entry Log) {
	m.writeSyncEntry(m.normalizeEntry(entry), true)
}

func (m *Module) normalizeEntry(entry Log) Log {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	if entry.Fields != nil {
		entry.Fields = cloneMap(entry.Fields)
	}
	entry = ensureIdentity(entry)
	return entry
}

func (m *Module) writeSyncEntry(entry Log, fallback bool) {
	if fallback {
		m.syncFallbackCount.Add(1)
	}
	start := time.Now()
	writeErrors := 0
	m.mutex.RLock()
	instances := m.instanceList
	m.mutex.RUnlock()
	for _, inst := range instances {
		if !inst.Allow(entry.Level, entry.Body, entry.Project, entry.Role, entry.Profile, entry.Node, entry.Fields) {
			continue
		}
		inst.writeMu.RLock()
		if err := inst.connect.Write(entry); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
			m.lastError.Store(err.Error())
			writeErrors++
		}
		inst.writeMu.RUnlock()
	}
	elapsed := time.Since(start)
	m.flushCount.Add(1)
	m.flushLogCount.Add(1)
	m.writeErrorCount.Add(int64(writeErrors))
	m.lastFlushLatencyMs.Store(elapsed.Milliseconds())
	m.lastDispatchLatencyMs.Store(elapsed.Milliseconds())
	m.totalDispatchNs.Add(elapsed.Nanoseconds())
}

func ensureIdentity(entry Log) Log {
	identity := infra.Identity()
	if strings.TrimSpace(entry.Project) == "" {
		entry.Project = identity.Project
	}
	if strings.TrimSpace(entry.Role) == "" {
		entry.Role = identity.Role
	}
	if strings.TrimSpace(entry.Profile) == "" {
		entry.Profile = identity.Profile
	}
	if strings.TrimSpace(entry.Node) == "" {
		entry.Node = identity.Node
	}
	return entry
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
	lastError, _ := m.lastError.Load().(string)

	return Map{
		"started":                  started,
		"connections_count":        connectionsCount,
		"queue_len":                queueLen,
		"queue_cap":                queueCap,
		"overflow":                 overflow,
		"batch":                    batch,
		"queued_count":             m.queuedCount.Load(),
		"sync_write_count":         m.syncFallbackCount.Load(),
		"sync_fallback_count":      m.syncFallbackCount.Load(),
		"drop_count":               m.dropCount.Load(),
		"last_drop_unix":           m.lastDropUnix.Load(),
		"flush_count":              flushCount,
		"flush_log_count":          m.flushLogCount.Load(),
		"write_error_count":        m.writeErrorCount.Load(),
		"last_error":               lastError,
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

func newInstanceWriter(module *Module, inst *Instance, mode overflowMode, queueSize int) *instanceWriter {
	if queueSize <= 0 {
		queueSize = 256
	}
	return &instanceWriter{
		module:   module,
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
				w.write(entries)
			case <-w.stopChan:
				for {
					select {
					case entries := <-w.queue:
						w.write(entries)
					default:
						return
					}
				}
			}
		}
	}()
}

func (w *instanceWriter) write(entries []Log) {
	defer w.module.putLogSlice(entries)
	if len(entries) == 0 {
		return
	}
	start := time.Now()
	w.instance.writeMu.RLock()
	if err := w.instance.connect.Write(entries...); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "log write failed:", err.Error())
		w.writeErrorCount.Add(1)
		w.lastError.Store(err.Error())
		w.module.recordWriteError(err)
	}
	w.instance.writeMu.RUnlock()
	elapsed := time.Since(start)
	w.lastWriteLatency.Store(elapsed.Milliseconds())
	w.totalWriteNs.Add(elapsed.Nanoseconds())
	w.writeCount.Add(1)
}

func (w *instanceWriter) stop() {
	close(w.stopChan)
	<-w.doneChan
}

func (w *instanceWriter) enqueue(entries []Log) int {
	if len(entries) == 0 {
		w.module.putLogSlice(entries)
		return 0
	}
	if w.mode == overflowBlock {
		var timer *time.Timer
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case w.queue <- entries:
				w.queuedCount.Add(int64(len(entries)))
				return 0
			default:
			}
			if timer == nil {
				timer = time.NewTimer(time.Millisecond)
			} else {
				timer.Reset(time.Millisecond)
			}
			select {
			case <-w.stopChan:
				dropped := len(entries)
				w.dropCount.Add(int64(dropped))
				w.module.markDropTime()
				w.module.putLogSlice(entries)
				return dropped
			case <-timer.C:
			}
		}
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
				w.module.markDropTime()
				w.module.putLogSlice(old)
				select {
				case w.queue <- entries:
					w.queuedCount.Add(int64(len(entries)))
					return dropped
				default:
					w.dropCount.Add(int64(len(entries)))
					w.module.markDropTime()
					w.module.putLogSlice(entries)
					return dropped + len(entries)
				}
			default:
			}
		}
		w.dropCount.Add(int64(len(entries)))
		w.module.markDropTime()
		w.module.putLogSlice(entries)
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
	lastError, _ := w.lastError.Load().(string)
	return Map{
		"queue_len":             queueLen,
		"queue_cap":             queueCap,
		"overflow":              overflowModeName(w.mode),
		"queued_count":          w.queuedCount.Load(),
		"drop_count":            w.dropCount.Load(),
		"write_error_count":     w.writeErrorCount.Load(),
		"last_error":            lastError,
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
		if verbCount, ok := formatArgCount(format); ok && verbCount > 0 && verbCount == (len(args)-1) {
			return fmt.Sprintf(format, args[1:]...)
		}
	}

	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%v", arg))
	}
	return strings.Join(parts, " ")
}

func formatArgCount(format string) (int, bool) {
	count := 0
	explicitMax := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i >= len(format) {
			return 0, false
		}
		if format[i] == '%' {
			continue
		}

		valueIndexed := false
		if next, index, ok := parseFormatIndex(format, i); ok {
			i = next
			valueIndexed = true
			if index > explicitMax {
				explicitMax = index
			}
		}
		for i < len(format) && strings.ContainsRune("#0+-", rune(format[i])) {
			i++
		}
		if i >= len(format) {
			return 0, false
		}
		if next, index, ok := parseFormatIndex(format, i); ok {
			i = next
			if i >= len(format) || format[i] != '*' {
				return 0, false
			}
			if index > explicitMax {
				explicitMax = index
			}
			i++
		} else if format[i] == '*' {
			count++
			i++
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		if i < len(format) && format[i] == '.' {
			i++
			if i >= len(format) {
				return 0, false
			}
			if next, index, ok := parseFormatIndex(format, i); ok {
				i = next
				if i >= len(format) || format[i] != '*' {
					return 0, false
				}
				if index > explicitMax {
					explicitMax = index
				}
				i++
			} else if format[i] == '*' {
				count++
				i++
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					i++
				}
			}
		}
		if i >= len(format) {
			return 0, false
		}
		if next, index, ok := parseFormatIndex(format, i); ok {
			i = next
			valueIndexed = true
			if index > explicitMax {
				explicitMax = index
			}
			if i >= len(format) {
				return 0, false
			}
		}
		if !strings.ContainsRune("vTtbcdoOqxXUeEfFgGsp", rune(format[i])) {
			return 0, false
		}
		if !valueIndexed {
			count++
		}
	}
	if explicitMax > count {
		count = explicitMax
	}
	return count, true
}

func parseFormatIndex(format string, pos int) (int, int, bool) {
	if pos >= len(format) || format[pos] != '[' {
		return pos, 0, false
	}
	end := pos + 1
	index := 0
	for end < len(format) && format[end] >= '0' && format[end] <= '9' {
		index = index*10 + int(format[end]-'0')
		end++
	}
	if end == pos+1 || end >= len(format) || format[end] != ']' || index <= 0 {
		return pos, 0, false
	}
	return end + 1, index, true
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
