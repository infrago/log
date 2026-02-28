package log

type (
	Driver interface {
		Connect(*Instance) (Connection, error)
	}

	Connection interface {
		Open() error
		Close() error
		Write(logs ...Log) error
	}
)
