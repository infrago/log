# log
infrago log module.

## API

- `log.Info(args...)` legacy flexible logging
- `log.Infof(format, args...)` format logging
- `log.Infow(body, fields)` structured logging

All levels support the same 3 styles:

- `Debug/Trace/Info/Notice/Warning/Error/Panic/Fatal`
- `Debugf/Tracef/Infof/Noticef/Warningf/Errorf/Panicf/Fatalf`
- `Debugw/Tracew/Infow/Noticew/Warningw/Errorw/Panicw/Fatalw`
- `Stats()`

Each log entry auto-includes identity fields in `fields`:

- `project`
- `profile`
- `node`

## Queue

- async queue + batch flush
- `buffer`: queue length
- `batch`: flush batch size
- `timeout`: flush interval
- `overflow`: `block` (default) / `drop` / `drop_newest` / `drop_oldest`
- `drop`: `old` (default) / `new` (works when `overflow = "drop"`)
