package log

import "errors"

const (
	NAME = "LOG"
)

var (
	//
	errInvalidLogConnection = errors.New("Invalid log connection.")
)

const (
	//LogLevel 日志级别，从小到大，数字越小越严重
	LevelFatal   Level = iota //错误级别，产生了严重错误，程序将退出
	LevelPanic                //恐慌级别，产生了恐慌，会调用panic
	LevelError                //错误级别，错误严重的
	LevelWarning              //警告级别，一般是记录调用
	LevelNotice               //注意级别，需要特别留意的信息
	LevelInfo                 //普通级别，普通信息
	LevelTrace                //追踪级别，主要是请求日志，调用追踪等
	LevelDebug                //调试级别，开发时输出调试时用，生产环境不建议
)

var (
	levelStrings = map[Level]string{
		LevelFatal:   "FATAL",
		LevelPanic:   "PANIC",
		LevelWarning: "WARNING",
		LevelError:   "ERROR",
		LevelNotice:  "NOTICE",
		LevelInfo:    "INFO",
		LevelTrace:   "TRACE",
		LevelDebug:   "DEBUG",
	}
)
