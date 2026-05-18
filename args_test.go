package log

import (
	"strings"
	"testing"
	"time"

	. "github.com/infrago/base"
)

func TestParseBodyLeavesLiteralPercentAlone(t *testing.T) {
	got := module.parseBody("disk 100% full", "node-1")
	want := "disk 100% full node-1"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseBodyFormatsValidPrintfPattern(t *testing.T) {
	got := module.parseBody("disk %d%% full", 100)
	want := "disk 100% full"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseBodyCountsStarWidthAndPrecisionArgs(t *testing.T) {
	got := module.parseBody("value %*.*f", 8, 2, 1.25)
	want := "value     1.25"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseBodySupportsIndexedPrintfPattern(t *testing.T) {
	got := module.parseBody("%[2]s %[1]s", "first", "second")
	want := "second first"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPanicValueUsesFieldsWhenBodyIsEmpty(t *testing.T) {
	got := panicValue("", Map{"x": 1})
	if got != "x=1" {
		t.Fatalf("expected fields as panic value, got %q", got)
	}
}

func TestBlockOverflowWriteStopsWaitingWhenStopped(t *testing.T) {
	m := &Module{
		configs:   map[string]Config{},
		drivers:   map[string]Driver{},
		instances: map[string]*Instance{},
		writers:   map[string]*instanceWriter{},
		queue:     make(chan Log, 1),
		stopChan:  make(chan struct{}),
		started:   true,
	}
	m.overflowMode = overflowBlock
	m.queue <- Log{Level: LevelInfo}

	done := make(chan struct{})
	go func() {
		m.Write(Log{Level: LevelInfo, Body: "blocked"})
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	close(m.stopChan)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected blocked write to stop waiting after stop signal")
	}
}

func TestConfigureAllowsZeroSample(t *testing.T) {
	m := &Module{
		configs:   map[string]Config{},
		drivers:   map[string]Driver{},
		instances: map[string]*Instance{},
		writers:   map[string]*instanceWriter{},
	}

	m.configure("default", Map{"sample": 0})
	m.Setup()

	if got := m.configs["default"].Sample; got != 0 {
		t.Fatalf("expected explicit zero sample to be preserved, got %v", got)
	}
}

func TestNormalizeConfigDefaultsZeroSampleToOne(t *testing.T) {
	cfg := normalizeConfig(Config{})
	if cfg.Sample != 1 {
		t.Fatalf("expected empty config sample to default to 1, got %v", cfg.Sample)
	}
}

func TestInstancePrepareMarksAllowAllFastPath(t *testing.T) {
	inst := &Instance{
		Config: normalizeConfig(Config{Level: LevelDebug, Sample: 1}),
	}
	inst.prepare()

	if !inst.allowAll() {
		t.Fatal("expected default normalized config to allow all levels without sampling")
	}
}

func TestJSONFormatFallsBackForUnmarshalableFields(t *testing.T) {
	inst := &Instance{
		Name: "default",
		Config: Config{
			Json:   true,
			Format: "%body%",
		},
	}
	entry := Log{
		Time:   time.Unix(1773000000, 0),
		Level:  LevelInfo,
		Body:   "hello",
		Fields: Map{"bad": func() {}},
	}

	got := inst.Format(entry)
	if got == "" {
		t.Fatal("expected non-empty log line")
	}
	if !strings.Contains(got, `"body":"hello"`) {
		t.Fatalf("expected log body to be preserved, got %s", got)
	}
	if !strings.Contains(got, `"_error":`) {
		t.Fatalf("expected marshal error to be recorded, got %s", got)
	}
}

func TestFatalUsesInjectableExitFunc(t *testing.T) {
	codeCh := make(chan int, 1)
	restore := SetExitFunc(func(code int) {
		codeCh <- code
	})
	defer restore()

	Fatal("fatal")

	select {
	case code := <-codeCh:
		if code != 1 {
			t.Fatalf("expected exit code 1, got %d", code)
		}
	default:
		t.Fatal("expected Fatal to call exitFunc")
	}
}

func TestStatsExposeOperationalFields(t *testing.T) {
	m := &Module{
		configs:   map[string]Config{},
		drivers:   map[string]Driver{},
		instances: map[string]*Instance{},
		writers:   map[string]*instanceWriter{},
	}
	m.recordDrop(2)
	m.recordWriteError(assertErr("boom"))
	m.syncFallbackCount.Store(3)

	stats := m.Stats()
	if stats["sync_write_count"] != int64(3) {
		t.Fatalf("expected sync_write_count, got %#v", stats["sync_write_count"])
	}
	if stats["last_error"] != "boom" {
		t.Fatalf("expected last_error, got %#v", stats["last_error"])
	}
	if stats["last_drop_unix"].(int64) == 0 {
		t.Fatal("expected last_drop_unix to be set")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
