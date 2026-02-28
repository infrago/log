package log

import "errors"

type Level = int

const (
	LevelFatal Level = iota
	LevelPanic
	LevelError
	LevelWarning
	LevelNotice
	LevelInfo
	LevelTrace
	LevelDebug
)

var (
	errInvalidLogDriver = errors.New("invalid log driver")
)

const (
	OverflowDrop       = "drop"
	OverflowDropNewest = "drop_newest"
	OverflowDropOldest = "drop_oldest"
	OverflowBlock      = "block"

	DropOld = "old"
	DropNew = "new"
)

var levelStrings = map[Level]string{
	LevelFatal:   "FATAL",
	LevelPanic:   "PANIC",
	LevelError:   "ERROR",
	LevelWarning: "WARNING",
	LevelNotice:  "NOTICE",
	LevelInfo:    "INFO",
	LevelTrace:   "TRACE",
	LevelDebug:   "DEBUG",
}
