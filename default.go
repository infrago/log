package log

import (
	"bytes"
	"io"
	"os"
)

func init() {
	module.RegisterDriver("default", &defaultDriver{})
}

type (
	defaultDriver struct{}

	defaultConnection struct {
		instance       *Instance
		stdout, stderr io.Writer
	}
)

func (d *defaultDriver) Connect(inst *Instance) (Connection, error) {
	return &defaultConnection{
		instance: inst,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
	}, nil
}

func (c *defaultConnection) Open() error  { return nil }
func (c *defaultConnection) Close() error { return nil }

func (c *defaultConnection) Write(logs ...Log) error {
	if len(logs) == 0 {
		return nil
	}

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	for _, entry := range logs {
		line := c.instance.Format(entry) + "\n"
		if entry.Level <= LevelWarning {
			_, _ = errBuf.WriteString(line)
		} else {
			_, _ = outBuf.WriteString(line)
		}
	}

	if errBuf.Len() > 0 {
		if _, err := c.stderr.Write(errBuf.Bytes()); err != nil {
			return err
		}
	}
	if outBuf.Len() > 0 {
		if _, err := c.stdout.Write(outBuf.Bytes()); err != nil {
			return err
		}
	}
	return nil
}
