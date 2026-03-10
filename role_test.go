package log

import (
	"strings"
	"testing"
	"time"
)

func TestFormatIncludesRole(t *testing.T) {
	inst := &Instance{
		Name: "default",
		Config: Config{
			Json:   true,
			Flag:   "app",
			Format: "%project% %role% %profile% %node% %body%",
		},
	}

	entry := Log{
		Time:    time.Unix(1773000000, 0),
		Level:   LevelInfo,
		Body:    "hello",
		Project: "demo",
		Role:    "site",
		Profile: "site-api",
		Node:    "n1",
	}

	jsonText := inst.Format(entry)
	if !strings.Contains(jsonText, `"role":"site"`) {
		t.Fatalf("expected json payload to include role, got %s", jsonText)
	}

	inst.Config.Json = false
	text := inst.Format(entry)
	if !strings.Contains(text, "site") {
		t.Fatalf("expected text format to include role, got %s", text)
	}
}

func TestHashUsesRole(t *testing.T) {
	a := hash01(LevelInfo, "default", "hello", "demo", "site", "site-api", "n1", nil)
	b := hash01(LevelInfo, "default", "hello", "demo", "worker", "site-api", "n1", nil)
	if a == b {
		t.Fatalf("expected different roles to affect sampling hash")
	}
}
