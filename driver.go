package log

type (

	// LogDriver log驱动
	Driver interface {
		// 连接到驱动
		Connect(*Instance) (Connect, error)
	}
	// LogConnect 日志连接
	Connect interface {
		// Open 打开连接
		Open() error

		// Close 关闭结束
		Close() error

		// Write 写入日志
		Write(Log) error

		// Flush 冲马桶
		Flush()
	}
)
