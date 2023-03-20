package log

import (
	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func init() {
	infra.Register(bridge)
}

var (
	bridge = &Bridge{}
)

type (
	Bridge struct{}
)

func (this *Bridge) Console(args ...Any) {
	module.Console(args...)
}
func (this *Bridge) Debug(args ...Any) {
	module.Debug(args...)
}
func (this *Bridge) Trace(args ...Any) {
	module.Trace(args...)
}
func (this *Bridge) Info(args ...Any) {
	module.Info(args...)
}
func (this *Bridge) Notice(args ...Any) {
	module.Notice(args...)
}
func (this *Bridge) Warning(args ...Any) {
	module.Warning(args...)
}
func (this *Bridge) Error(args ...Any) {
	module.Error(args...)
}
func (this *Bridge) Panic(args ...Any) {
	module.Panic(args...)
}
func (this *Bridge) Fatal(args ...Any) {
	module.Fatal(args...)
}
