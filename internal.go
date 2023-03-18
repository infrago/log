package log

import (
	"fmt"
	"log"
	"strings"

	. "github.com/infrago/base"
)

// string 把对象转成字串
func (this *Module) string(args ...Any) string {
	vs := []string{}
	for _, v := range args {
		s := fmt.Sprintf("%v", v)
		vs = append(vs, s)
	}
	return strings.Join(vs, " ")
}
func (this *Module) parse(args ...Any) string {
	ls := len(args)
	if ls == 0 {
		return ""
	}
	if ls == 1 {
		return this.string(args...)
	} else {
		if format, ok := args[0].(string); ok {
			ccc := strings.Count(format, "%") - strings.Count(format, "%%")
			if ccc > 0 && ccc == (len(args)-1) {
				return fmt.Sprintf(format, args[1:]...)
			}
		}
		return this.string(args...)
	}
}

// output 是为了直接输出到控制台，不管是否启用控制台
func (this *Module) output(args ...Any) {
	body := this.parse(args...)
	log.Println(body)
}

// 输出控制台信息
func (this *Module) Console(args ...Any) {
	this.output(args...)
}

// 调试
func (this *Module) Debug(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelDebug, this.parse(args...))
	}
}
func (this *Module) Trace(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelTrace, this.parse(args...))
	}
}
func (this *Module) Info(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelInfo, this.parse(args...))
	}
}
func (this *Module) Notice(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelNotice, this.parse(args...))
	}
}
func (this *Module) Warning(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelWarning, this.parse(args...))
	}
}
func (this *Module) Error(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelError, this.parse(args...))
	}
}
func (this *Module) Panic(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelPanic, this.parse(args...))
	}
	panic(this.parse(args...))
}
func (this *Module) Fatal(args ...Any) {
	if len(this.instances) == 0 {
		this.output(args...)
	} else {
		this.Logging(LevelFatal, this.parse(args...))
	}
	//待处理，发送退出信号
}
