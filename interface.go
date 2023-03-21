package log

import (
	"strings"

	. "github.com/infrago/base"
	"github.com/infrago/infra"
)

func (this *Module) Register(name string, value Any) {
	switch config := value.(type) {
	case Driver:
		this.Driver(name, config)
	case Config:
		this.Config(name, config)
	case Configs:
		this.Configs(config)
	}
}

func (this *Module) configure(name string, config Map) {
	cfg := Config{
		Driver: infra.DEFAULT, Level: LevelDebug, Levels: map[Level]bool{},
	}
	//如果已经存在了，用现成的改写
	if vv, ok := this.configs[name]; ok {
		cfg = vv
	}

	//设置驱动
	if driver, ok := config["driver"].(string); ok {
		cfg.Driver = driver
	}
	//设置级别
	if level, ok := config["level"].(string); ok {
		for l, s := range levelStrings {
			if strings.ToUpper(level) == s {
				cfg.Level = l
			}
		}
	}

	if levels, ok := config["levels"].([]Any); ok {
		for _, lllll := range levels {
			if level, ok := lllll.(string); ok {
				for l, s := range levelStrings {
					if strings.ToUpper(level) == s {
						cfg.Levels[l] = true
					}
				}
			}
		}
	}

	//是否json
	if json, ok := config["json"].(bool); ok {
		cfg.Json = json
	}
	//设置是否同步
	if sync, ok := config["sync"].(bool); ok {
		cfg.Sync = sync
	}
	// 设置输出格式
	if flag, ok := config["flag"].(string); ok {
		cfg.Flag = flag
	}
	// 设置输出格式
	if format, ok := config["format"].(string); ok {
		cfg.Format = format
	}

	// 设置缓存池大小
	if pool, ok := config["pool"].(int64); ok && pool > 0 {
		cfg.Pool = int(pool)
	}
	if pool, ok := config["pool"].(int); ok && pool > 0 {
		cfg.Pool = pool
	}

	if setting, ok := config["setting"].(Map); ok {
		cfg.Setting = setting
	}

	//保存配置
	this.configs[name] = cfg
}
func (this *Module) Configure(global Map) {
	var config Map
	if vvv, ok := global["log"].(Map); ok {
		config = vvv
	}
	if config == nil {
		return
	}

	//记录上一层的配置，如果有的话
	rootConfig := Map{}

	for key, val := range config {
		if conf, ok := val.(Map); ok {
			this.configure(key, conf)
		} else {
			rootConfig[key] = val
		}
	}

	if len(rootConfig) > 0 {
		this.configure(infra.DEFAULT, rootConfig)
	}
}

func (this *Module) Initialize() {
	if this.initialized {
		return
	}

	// 如果没有配置时，默认一个
	if len(this.configs) == 0 {
		config := Config{
			Driver: infra.DEFAULT, Format: "%time% [%level%] %body%",
			Level: LevelDebug, Levels: map[Level]bool{},
		}
		for level, _ := range levelStrings {
			config.Levels[level] = true
		}

		this.configs[infra.DEFAULT] = config
	} else {
		for key, config := range this.configs {
			if config.Driver == "" {
				config.Driver = infra.DEFAULT
			}
			if config.Format == "" {
				config.Format = "%time% [%level%] %body%"
			}

			if len(config.Levels) == 0 {
				for level, _ := range levelStrings {
					if config.Level >= level {
						config.Levels[level] = true
					}
				}
			}

			this.configs[key] = config
		}
	}

	this.initialized = true
}
func (this *Module) Connect() {
	if this.connected {
		return
	}

	// driver, ok := this.drivers[this.config.Driver]
	// if ok == false {
	// 	panic("Invalid log driver: " + this.config.Driver)
	// }

	// // 建立连接
	// connect, err := driver.Connect(this.config)
	// if err != nil {
	// 	panic("Failed to connect to log: " + err.Error())
	// }

	// // 打开连接
	// err = connect.Open()
	// if err != nil {
	// 	panic("Failed to open log connect: " + err.Error())
	// }

	for name, config := range this.configs {
		driver, ok := this.drivers[config.Driver]
		if ok == false {
			panic("Invalid log driver: " + config.Driver)
		}

		inst := &Instance{
			nil, name, config, config.Setting,
		}

		// 建立连接
		connect, err := driver.Connect(inst)
		if err != nil {
			panic("Failed to connect to log: " + err.Error())
		}

		// 打开连接
		err = connect.Open()
		if err != nil {
			panic("Failed to open log connect: " + err.Error())
		}

		inst.connect = connect

		//保存实例
		this.instances[name] = inst
	}

	this.connected = true
}
func (this *Module) Launch() {
	if this.launched {
		return
	}

	this.launched = true
}
func (this *Module) Terminate() {
	for _, ins := range this.instances {
		ins.connect.Close()
	}

	this.launched = false
	this.connected = false
	this.initialized = false
}
