package log

import (
	"sync"
	"time"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func init() {
	infra.Mount(module)
}

var (
	module = &Module{
		configs:   make(map[string]Config, 0),
		drivers:   make(map[string]Driver, 0),
		instances: make(map[string]*Instance, 0),
	}
)

type (
	// Level 日志级别，从小到大，数字越小越严重
	Level = int

	// 日志模块定义
	Module struct {
		//mutex 锁
		mutex sync.Mutex

		// 几项运行状态
		connected, initialized, launched bool

		configs   map[string]Config
		drivers   map[string]Driver
		instances map[string]*Instance

		waiter sync.WaitGroup

		// logger 日志发送管道
		logger chan *Log

		// signal 信号管道，用于flush缓存区，或是结束循环
		// false 表示flush缓存区
		// true 表示结束关闭循环
		signal chan bool
	}

	// LogConfig 日志模块配置
	Configs map[string]Config
	Config  struct {
		// Driver 日志驱动，默认为 default
		Driver string

		// Level 输出的日志级别
		// fatal, panic, warning, notice, info, trace, debug
		Level Level

		Levels map[Level]bool

		// Json 是否开启json输出模式
		// 开启后，所有日志 body 都会被包装成json格式输出
		Json bool

		// Sync 是否开启同步输出，默认为false，表示异步输出
		// 注意：如果开启同步输出，有可能影响程序性能
		Sync bool

		// Pool 异步缓冲池大小
		Pool int

		//Flag 标记
		// 默认为infra.Role()，表名当前节点的角色
		Flag string `toml:"flag"`

		// Format 日志输出格式，默认格式为 %time% [%level%] %body%
		// 可选参数，参数使用 %% 包裹，如 %time%
		// time		格式化后的时间，如：2006-01-02 15:03:04.000
		// unix		unix时间戳，如：1650271473
		// level	日志级别，如：TRACE
		// body		日志内容
		Format string `toml:"format"`

		// Setting 是为不同驱动准备的自定义参数
		// 具体参数表，请参考各不同的驱动
		Setting Map `toml:"setting"`
	}
)

// Driver 为log模块注册驱动
func (this *Module) Driver(name string, driver Driver) {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if driver == nil {
		panic("Invalid log driver: " + name)
	}

	if infra.Override() {
		this.drivers[name] = driver
	} else {
		if this.drivers[name] == nil {
			this.drivers[name] = driver
		}
	}
}

func (this *Module) Config(name string, config Config) {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if name == "" {
		name = infra.DEFAULT
	}

	if infra.Override() {
		this.configs[name] = config
	} else {
		if _, ok := this.configs[name]; ok == false {
			this.configs[name] = config
		}
	}
}
func (this *Module) Configs(config Configs) {
	for key, val := range config {
		this.Config(key, val)
	}
}

// Write 写入日志，对外的，需要处理逻辑
func (this *Module) Write(log Log) {
	for _, inst := range this.instances {
		// if log.Level > inst.Config.Level {
		// 	return
		// }
		//自定义级别的输出
		if yes, ok := inst.Config.Levels[log.Level]; ok && yes {
			inst.connect.Write(log)
		}
	}
}

func (this *Module) Flush() {
	for _, inst := range this.instances {
		inst.connect.Flush()
	}
}

// Logging 对外按日志级写日志的方法
func (this *Module) Logging(level Level, body string) {
	log := Log{Time: time.Now().UnixNano(), Level: level, Body: body}
	this.Write(log)
}
