package main

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sagikazarmark/slog-shim"
)

type options struct {
	host        string
	port        string
	user        string
	password    string
	keyFile     string
	command     string
	script      string
	scriptDir   string
	listScripts bool
	listFailed  bool
	listHosts   bool
	usePty      bool
	jumpHost    string
	jumpUser    string
	jumpKey     string
	jumpPasswd  string
	group       string
	tag         string
	batch       bool
	workers     int
}

func parseFlags() (options, string) {
	var opt options
	var configFile string

	exPath, err := os.Executable()
	defaultConfig := "config.yaml"
	if err == nil {
		defaultConfig = filepath.Join(filepath.Dir(exPath), "config.yaml")
	}

	flag.StringVar(&configFile, "config", defaultConfig, "配置文件路径")
	flag.StringVar(&opt.host, "host", "", "远程主机地址或配置文件中的主机名")
	flag.StringVar(&opt.port, "port", "", "远程主机端口")
	flag.StringVar(&opt.user, "user", "", "登录用户名")
	flag.StringVar(&opt.password, "password", "", "登录密码")
	flag.StringVar(&opt.keyFile, "key", "", "私钥文件路径")
	flag.StringVar(&opt.command, "cmd", "", "要执行的命令")
	flag.StringVar(&opt.script, "script", "", "要执行的脚本文件名")
	flag.StringVar(&opt.scriptDir, "script-dir", "", "脚本目录")
	flag.BoolVar(&opt.listScripts, "list-scripts", false, "列出可用的脚本文件")
	flag.BoolVar(&opt.listFailed, "list-failed", false, "列出执行失败的环境")
	flag.BoolVar(&opt.listHosts, "list-hosts", false, "列出所有配置的主机")
	flag.BoolVar(&opt.usePty, "pty", false, "是否申请伪终端")
	flag.StringVar(&opt.jumpHost, "jump", "", "跳板机地址 host:port")
	flag.StringVar(&opt.jumpUser, "jump-user", "", "跳板机用户名")
	flag.StringVar(&opt.jumpKey, "jump-key", "", "跳板机私钥")
	flag.StringVar(&opt.jumpPasswd, "jump-pass", "", "跳板机密码")
	flag.StringVar(&opt.group, "group", "", "环境组名称（批量执行）")
	flag.StringVar(&opt.tag, "tag", "", "标签筛选（按标签批量执行）")
	flag.BoolVar(&opt.batch, "batch", false, "批量执行所有主机")
	flag.IntVar(&opt.workers, "workers", 0, "并行执行的并发数（0表示自动根据CPU核心数计算）")
	flag.Parse()
	return opt, configFile
}

func main() {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		slog.Warn("加载时区失败，使用UTC+8偏移量", "error", err)
		loc = time.FixedZone("CST", 8*60*60)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				t := a.Value.Time().In(loc)
				return slog.Attr{Key: slog.TimeKey, Value: slog.StringValue(t.Format("2006-01-02 15:04:05"))}
			}
			return a
		},
	})))

	slog.Info("启动 shlink...")

	if err := initConfig(); err != nil {
		slog.Error("初始化配置失败", "error", err)
		os.Exit(1)
	}

	opt, configFile := parseFlags()
	slog.Info("配置文件路径", "path", configFile)
	slog.Info("命令行指定的主机", "host", opt.host)

	var config *Config
	var loadErr error
	if _, err := os.Stat(configFile); err == nil {
		config, loadErr = loadConfig(configFile)
		if loadErr != nil {
			slog.Warn("加载配置文件失败", "error", loadErr, "message", "将使用命令行参数")
		} else {
			slog.Info("配置文件加载成功")

			if opt.host == "" && config.Global.DefaultHost != "" {
				opt.host = config.Global.DefaultHost
				slog.Info("使用默认主机", "host", opt.host)
			}
		}
	}

	if opt.listScripts {
		if opt.scriptDir == "" && config != nil {
			opt.scriptDir = config.Global.ScriptDir
		}
		if opt.scriptDir == "" {
			opt.scriptDir = "scripts"
		}
		listScripts(opt.scriptDir)
		return
	}

	if opt.listFailed {
		listFailed()
		return
	}

	if opt.listHosts {
		listHosts(config)
		return
	}

	var targetHosts []string
	if opt.batch {
		targetHosts = getAllHosts(config)
	} else if opt.group != "" {
		targetHosts = getHostsByGroup(config, opt.group)
	} else if opt.tag != "" {
		targetHosts = getHostsByTag(config, opt.tag)
	} else if opt.host != "" {
		targetHosts = []string{opt.host}
	} else if config != nil && config.Global.DefaultHost != "" {
		targetHosts = []string{config.Global.DefaultHost}
	} else {
		slog.Error("主机参数必填", "usage", "使用 -host 指定主机、-group 指定环境组、-tag 指定标签或 -batch 批量执行")
		os.Exit(1)
	}

	slog.Info("目标主机列表", "count", len(targetHosts), "hosts", strings.Join(targetHosts, ", "))

	if opt.batch && config != nil && config.Global.Parallel {
		executeParallel(config, targetHosts, opt)
	} else {
		for _, hostName := range targetHosts {
			slog.Info("========================================")
			slog.Info("处理主机", "host", hostName)
			executeOnHost(config, hostName, opt)
		}
	}

	slog.Info("所有主机处理完成")
}

func executeParallel(config *Config, hosts []string, baseOpt options) {
	cpuCount := runtime.NumCPU()
	maxWorkers := 0

	if baseOpt.workers > 0 {
		maxWorkers = baseOpt.workers
		slog.Info("使用命令行指定的并发数", "workers", maxWorkers)
	} else if config != nil && config.Global.ParallelWorkers > 0 {
		maxWorkers = config.Global.ParallelWorkers
		slog.Info("使用配置文件指定的并发数", "workers", maxWorkers)
	} else if cpuCount > 0 {
		maxWorkers = cpuCount * 2
		slog.Info("自动计算并发数", "cpu_cores", cpuCount, "workers", maxWorkers)
	}

	if maxWorkers <= 0 {
		slog.Warn("无法确定并发数，回退到串行执行")
		for _, hostName := range hosts {
			slog.Info("========================================")
			slog.Info("串行处理主机", "host", hostName)
			executeOnHost(config, hostName, baseOpt)
		}
		return
	}

	if maxWorkers > len(hosts) {
		maxWorkers = len(hosts)
	}

	slog.Info("并行执行模式", "max_workers", maxWorkers, "target_hosts", len(hosts))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)

	for _, hostName := range hosts {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			slog.Info("========================================")
			slog.Info("并行处理主机", "host", name)
			executeOnHost(config, name, baseOpt)
		}(hostName)
	}

	wg.Wait()
}

func executeOnHost(config *Config, hostName string, baseOpt options) {
	opt := baseOpt
	opt.host = hostName

	if config != nil {
		if hostConfig, ok := config.Hosts[hostName]; ok {
			slog.Info("从配置文件加载主机", "host", hostName)
			if opt.port == "" {
				opt.port = hostConfig.Port
			}
			if opt.user == "" {
				opt.user = hostConfig.User
			}
			if opt.password == "" {
				opt.password = hostConfig.Password
			}
			if opt.keyFile == "" {
				opt.keyFile = hostConfig.KeyFile
			}
			if opt.jumpHost == "" {
				opt.jumpHost = hostConfig.JumpHost
			}
			if opt.jumpUser == "" {
				opt.jumpUser = hostConfig.JumpUser
			}
			if opt.jumpKey == "" {
				opt.jumpKey = hostConfig.JumpKey
			}
			if opt.jumpPasswd == "" {
				opt.jumpPasswd = hostConfig.JumpPasswd
			}
			if opt.scriptDir == "" {
				opt.scriptDir = hostConfig.ScriptDir
			}
			if !opt.usePty && config.Global.UsePty {
				opt.usePty = config.Global.UsePty
			}
			if opt.scriptDir == "" {
				opt.scriptDir = config.Global.ScriptDir
			}
			opt.host = hostConfig.Host
		}
	}

	if opt.port == "" {
		opt.port = "22"
	}
	if opt.user == "" {
		opt.user = "root"
	}
	if opt.scriptDir == "" {
		opt.scriptDir = "scripts"
	}

	client, err := dial(opt)
	if err != nil {
		slog.Error("连接失败", "error", err)
		saveSummary(hostName, "connection", "FAILED", err.Error())
		saveFailedMarker(hostName)
		return
	}
	defer client.Close()
	slog.Info("已连接到主机", "host", opt.host, "port", opt.port)

	if opt.command != "" {
		slog.Info("执行命令", "command", opt.command)
		if err := runCommand(client, opt); err != nil {
			slog.Error("命令执行失败", "error", err)
			saveSummary(hostName, opt.command, "FAILED", err.Error())
			saveFailedMarker(hostName)
		} else {
			saveSummary(hostName, opt.command, "SUCCESS", "")
		}
	} else {
		var scripts []string
		if opt.script != "" {
			scripts = []string{opt.script}
		} else {
			files, err := os.ReadDir(opt.scriptDir)
			if err != nil {
				slog.Error("读取脚本目录失败", "error", err)
				return
			}
			for _, file := range files {
				if !file.IsDir() && strings.HasSuffix(file.Name(), ".sh") {
					scripts = append(scripts, file.Name())
				}
			}
			if len(scripts) == 0 {
				slog.Warn("脚本目录为空", "directory", opt.scriptDir)
				return
			}
			slog.Info("发现脚本文件", "count", len(scripts), "scripts", strings.Join(scripts, ", "))
		}

		for _, scriptName := range scripts {
			scriptPath := filepath.Join(opt.scriptDir, scriptName)
			scriptContent, err := os.ReadFile(scriptPath)
			if err != nil {
				slog.Error("读取脚本文件失败", "error", err, "file", scriptPath)
				saveSummary(hostName, scriptName, "FAILED", err.Error())
				saveFailedMarker(hostName)
				continue
			}

			slog.Info("执行脚本", "script", scriptName)
			output, err := runScript(client, scriptContent)
			if err != nil {
				slog.Error("脚本执行失败", "script", scriptName, "error", err)
				saveReport(hostName, scriptName, output)
				saveSummary(hostName, scriptName, "FAILED", err.Error())
				saveFailedMarker(hostName)
			} else {
				saveReport(hostName, scriptName, output)
				saveSummary(hostName, scriptName, "SUCCESS", "")
			}
		}
	}
}

func listHosts(config *Config) {
	if config == nil {
		slog.Warn("配置文件未加载")
		return
	}

	slog.Info("已配置的主机列表:")
	for name, hostConfig := range config.Hosts {
		tags := ""
		if len(hostConfig.Tags) > 0 {
			tags = " [" + strings.Join(hostConfig.Tags, ", ") + "]"
		}
		slog.Info("主机", "name", name, "host", hostConfig.Host, "tags", tags)
	}

	if len(config.EnvGroups) > 0 {
		slog.Info("环境组列表:")
		for _, group := range config.EnvGroups {
			slog.Info("环境组", "name", group.Name, "hosts", strings.Join(group.Hosts, ", "))
		}
	}
}

func getAllHosts(config *Config) []string {
	if config == nil {
		return []string{}
	}
	var hosts []string
	for name := range config.Hosts {
		hosts = append(hosts, name)
	}
	return hosts
}

func getHostsByGroup(config *Config, groupName string) []string {
	if config == nil {
		return []string{}
	}
	for _, group := range config.EnvGroups {
		if group.Name == groupName {
			return group.Hosts
		}
	}
	slog.Warn("环境组未找到", "group", groupName)
	return []string{}
}

func getHostsByTag(config *Config, tag string) []string {
	if config == nil {
		return []string{}
	}
	var hosts []string
	for name, hostConfig := range config.Hosts {
		for _, t := range hostConfig.Tags {
			if t == tag {
				hosts = append(hosts, name)
				break
			}
		}
	}
	return hosts
}