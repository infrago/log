package log

import (
	"fmt"
	"os"
	"sync"
)

import . "github.com/infrago/base"

var (
	exitFuncMu sync.RWMutex
	exitFunc   = os.Exit
)

func SetExitFunc(fn func(int)) func() {
	if fn == nil {
		fn = os.Exit
	}
	exitFuncMu.Lock()
	previous := exitFunc
	exitFunc = fn
	exitFuncMu.Unlock()
	return func() {
		SetExitFunc(previous)
	}
}

func callExit(code int) {
	exitFuncMu.RLock()
	fn := exitFunc
	exitFuncMu.RUnlock()
	fn(code)
}

func Levels() map[Level]string {
	out := make(map[Level]string, len(levelStrings))
	for k, v := range levelStrings {
		out[k] = v
	}
	return out
}

func Write(level Level, args ...Any) {
	module.Logging(level, args...)
}

func Writef(level Level, format string, args ...Any) {
	module.Loggingf(level, format, args...)
}

func Writew(level Level, body string, fields Map) {
	module.Loggingw(level, body, fields)
}

func Debug(args ...Any)   { module.Logging(LevelDebug, args...) }
func Trace(args ...Any)   { module.Logging(LevelTrace, args...) }
func Info(args ...Any)    { module.Logging(LevelInfo, args...) }
func Notice(args ...Any)  { module.Logging(LevelNotice, args...) }
func Warning(args ...Any) { module.Logging(LevelWarning, args...) }
func Error(args ...Any)   { module.Logging(LevelError, args...) }

func Debugf(format string, args ...Any)   { module.Loggingf(LevelDebug, format, args...) }
func Tracef(format string, args ...Any)   { module.Loggingf(LevelTrace, format, args...) }
func Infof(format string, args ...Any)    { module.Loggingf(LevelInfo, format, args...) }
func Noticef(format string, args ...Any)  { module.Loggingf(LevelNotice, format, args...) }
func Warningf(format string, args ...Any) { module.Loggingf(LevelWarning, format, args...) }
func Errorf(format string, args ...Any)   { module.Loggingf(LevelError, format, args...) }

func Debugw(body string, fields Map)   { module.Loggingw(LevelDebug, body, fields) }
func Tracew(body string, fields Map)   { module.Loggingw(LevelTrace, body, fields) }
func Infow(body string, fields Map)    { module.Loggingw(LevelInfo, body, fields) }
func Noticew(body string, fields Map)  { module.Loggingw(LevelNotice, body, fields) }
func Warningw(body string, fields Map) { module.Loggingw(LevelWarning, body, fields) }
func Errorw(body string, fields Map)   { module.Loggingw(LevelError, body, fields) }

func Panic(args ...Any) {
	body, fields := module.parseArgs(args...)
	module.WriteSync(Log{Level: LevelPanic, Body: body, Fields: fields})
	panic(panicValue(body, fields))
}
func Panicf(format string, args ...Any) {
	body := fmt.Sprintf(format, args...)
	module.WriteSync(Log{Level: LevelPanic, Body: body})
	panic(body)
}
func Panicw(body string, fields Map) {
	module.WriteSync(Log{Level: LevelPanic, Body: body, Fields: fields})
	panic(panicValue(body, fields))
}

func Fatal(args ...Any) {
	body, fields := module.parseArgs(args...)
	module.WriteSync(Log{Level: LevelFatal, Body: body, Fields: fields})
	callExit(1)
}
func Fatalf(format string, args ...Any) {
	module.WriteSync(Log{Level: LevelFatal, Body: fmt.Sprintf(format, args...)})
	callExit(1)
}
func Fatalw(body string, fields Map) {
	module.WriteSync(Log{Level: LevelFatal, Body: body, Fields: fields})
	callExit(1)
}

func Stats() Map {
	return module.Stats()
}

func panicValue(body string, fields Map) string {
	if body != "" || len(fields) == 0 {
		return body
	}
	return formatFields(fields)
}
