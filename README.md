# log

`log` 是 infrago 的模块包。

## 安装

```bash
go get github.com/infrago/log@latest
```

## 最小接入

```go
package main

import (
    _ "github.com/infrago/log"
    "github.com/infrago/infra"
)

func main() {
    infra.Run()
}
```

## 配置示例

```toml
[log]
driver = "default"
```

## 公开 API（摘自源码）

- `func (inst *Instance) Format(entry Log) string`
- `func (inst *Instance) Allow(level Level, body, project, profile, node string, fields Map) bool`
- `func (d *defaultDriver) Connect(inst *Instance) (Connection, error)`
- `func (c *defaultConnection) Open() error  { return nil }`
- `func (c *defaultConnection) Close() error { return nil }`
- `func (c *defaultConnection) Write(logs ...Log) error`
- `func Levels() map[Level]string`
- `func Write(level Level, args ...Any)`
- `func Writef(level Level, format string, args ...Any)`
- `func Writew(level Level, body string, fields Map)`
- `func Debug(args ...Any)   { module.Logging(LevelDebug, args...) }`
- `func Trace(args ...Any)   { module.Logging(LevelTrace, args...) }`
- `func Info(args ...Any)    { module.Logging(LevelInfo, args...) }`
- `func Notice(args ...Any)  { module.Logging(LevelNotice, args...) }`
- `func Warning(args ...Any) { module.Logging(LevelWarning, args...) }`
- `func Error(args ...Any)   { module.Logging(LevelError, args...) }`
- `func Debugf(format string, args ...Any)   { module.Loggingf(LevelDebug, format, args...) }`
- `func Tracef(format string, args ...Any)   { module.Loggingf(LevelTrace, format, args...) }`
- `func Infof(format string, args ...Any)    { module.Loggingf(LevelInfo, format, args...) }`
- `func Noticef(format string, args ...Any)  { module.Loggingf(LevelNotice, format, args...) }`
- `func Warningf(format string, args ...Any) { module.Loggingf(LevelWarning, format, args...) }`
- `func Errorf(format string, args ...Any)   { module.Loggingf(LevelError, format, args...) }`
- `func Debugw(body string, fields Map)   { module.Loggingw(LevelDebug, body, fields) }`
- `func Tracew(body string, fields Map)   { module.Loggingw(LevelTrace, body, fields) }`
- `func Infow(body string, fields Map)    { module.Loggingw(LevelInfo, body, fields) }`
- `func Noticew(body string, fields Map)  { module.Loggingw(LevelNotice, body, fields) }`
- `func Warningw(body string, fields Map) { module.Loggingw(LevelWarning, body, fields) }`
- `func Errorw(body string, fields Map)   { module.Loggingw(LevelError, body, fields) }`
- `func Panic(args ...Any)`
- `func Panicf(format string, args ...Any)`
- `func Panicw(body string, fields Map)`
- `func Fatal(args ...Any) { module.Logging(LevelFatal, args...) }`
- `func Fatalf(format string, args ...Any)`
- `func Fatalw(body string, fields Map)`
- `func Stats() Map`
- `func (m *Module) Register(name string, value Any)`
- `func (m *Module) RegisterDriver(name string, driver Driver)`
- `func (m *Module) RegisterConfig(name string, cfg Config)`
- `func (m *Module) RegisterConfigs(configs Configs)`
- `func (m *Module) Config(global Map)`

## 排错

- 模块未运行：确认空导入已存在
- driver 无效：确认驱动包已引入
- 配置不生效：检查配置段名是否为 `[log]`
