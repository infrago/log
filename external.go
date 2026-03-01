package log

import "fmt"

import . "github.com/infrago/base"

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
	module.Logging(LevelPanic, args...)
	panic(module.parseBody(args...))
}
func Panicf(format string, args ...Any) {
	module.Loggingf(LevelPanic, format, args...)
	panic(fmt.Sprintf(format, args...))
}
func Panicw(body string, fields Map) {
	module.Loggingw(LevelPanic, body, fields)
	panic(body)
}

func Fatal(args ...Any) { module.Logging(LevelFatal, args...) }
func Fatalf(format string, args ...Any) {
	module.Loggingf(LevelFatal, format, args...)
}
func Fatalw(body string, fields Map) {
	module.Loggingw(LevelFatal, body, fields)
}

func Stats() Map {
	return module.Stats()
}
