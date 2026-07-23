package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"golang.org/x/crypto/ssh"
)

type options struct {
	host       string
	port       string
	user       string
	password   string
	keyFile    string
	command    string
	script     string
	scriptDir  string
	listScripts bool
	usePty     bool
	jumpHost   string
	jumpUser   string
	jumpKey    string
	jumpPasswd string
}

type HostConfig struct {
	Host         string `yaml:"host"`
	Port         string `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	KeyFile      string `yaml:"key_file"`
	JumpHost     string `yaml:"jump_host"`
	JumpUser     string `yaml:"jump_user"`
	JumpKey      string `yaml:"jump_key"`
	JumpPasswd   string `yaml:"jump_passwd"`
	ScriptDir    string `yaml:"script_dir"`
	DefaultScript string `yaml:"default_script"`
}

type GlobalConfig struct {
	Timeout      int  `yaml:"timeout"`
	UsePty       bool `yaml:"use_pty"`
	ScriptDir    string `yaml:"script_dir"`
	DefaultHost  string `yaml:"default_host"`
}

type Config struct {
	Hosts  map[string]HostConfig `yaml:"hosts"`
	Global GlobalConfig          `yaml:"global"`
}

func loadConfig(configFile string) (*Config, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

func parseFlags() (options, string) {
	var opt options
	var configFile string
	flag.StringVar(&configFile, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&opt.host, "host", "", "远程主机地址或配置文件中的主机名 (必填)")
	flag.StringVar(&opt.port, "port", "", "远程主机端口")
	flag.StringVar(&opt.user, "user", "", "登录用户名")
	flag.StringVar(&opt.password, "password", "", "登录密码")
	flag.StringVar(&opt.keyFile, "key", "", "私钥文件路径 (例如 ~/.ssh/id_rsa)")
	flag.StringVar(&opt.command, "cmd", "", "要执行的命令")
	flag.StringVar(&opt.script, "script", "", "要执行的脚本文件名（在 script_dir 目录下）")
	flag.StringVar(&opt.scriptDir, "script-dir", "", "脚本目录")
	flag.BoolVar(&opt.listScripts, "list-scripts", false, "列出可用的脚本文件")
	flag.BoolVar(&opt.usePty, "pty", false, "是否申请伪终端 (sudo 等交互场景)")
	flag.StringVar(&opt.jumpHost, "jump", "", "跳板机地址 host:port")
	flag.StringVar(&opt.jumpUser, "jump-user", "", "跳板机用户名")
	flag.StringVar(&opt.jumpKey, "jump-key", "", "跳板机私钥")
	flag.StringVar(&opt.jumpPasswd, "jump-pass", "", "跳板机密码")
	flag.Parse()
	return opt, configFile
}

func buildAuthMethods(password, keyFile string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if keyFile != "" {
		if strings.HasPrefix(keyFile, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		keyFile = filepath.Join(home, keyFile[2:])
		}

		key, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("读取私钥失败: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if password != "" {
		methods = append(methods, ssh.Password(password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("至少指定 -password 或 -key 中的一种认证方式")
	}
	return methods, nil
}

func sshConfig(user, password, keyFile string) (*ssh.ClientConfig, error) {
	methods, err := buildAuthMethods(password, keyFile)
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		Timeout:         10 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 生产环境建议替换为 knownhosts 校验
	}, nil
}

func dial(opt options) (*ssh.Client, error) {
	target := opt.host + ":" + opt.port

	if opt.jumpHost == "" {
		config, err := sshConfig(opt.user, opt.password, opt.keyFile)
		if err != nil {
			return nil, err
		}
		return ssh.Dial("tcp", target, config)
	}

	// 跳板机连接
	if opt.jumpUser == "" {
		opt.jumpUser = opt.user
	}
	if opt.jumpKey == "" {
		opt.jumpKey = opt.keyFile
	}
	if opt.jumpPasswd == "" {
		opt.jumpPasswd = opt.password
	}

	jumpConfig, err := sshConfig(opt.jumpUser, opt.jumpPasswd, opt.jumpKey)
	if err != nil {
		return nil, fmt.Errorf("跳板机配置失败: %w", err)
	}
	if !strings.Contains(opt.jumpHost, ":") {
		opt.jumpHost += ":22"
	}
	jumpClient, err := ssh.Dial("tcp", opt.jumpHost, jumpConfig)
	if err != nil {
		return nil, fmt.Errorf("连接跳板机失败: %w", err)
	}

	conn, err := jumpClient.Dial("tcp", target)
	if err != nil {
		jumpClient.Close()
		return nil, fmt.Errorf("跳板机转发目标失败: %w", err)
	}

	targetConfig, err := sshConfig(opt.user, opt.password, opt.keyFile)
	if err != nil {
		jumpClient.Close()
		conn.Close()
		return nil, err
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, target, targetConfig)
	if err != nil {
		jumpClient.Close()
		conn.Close()
		return nil, fmt.Errorf("目标机 SSH 握手失败: %w", err)
	}
	return ssh.NewClient(ncc, chans, reqs), nil
}

func runCommand(client *ssh.Client, opt options) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if opt.usePty {
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
			return fmt.Errorf("请求伪终端失败: %w", err)
		}

		stdin, err := session.StdinPipe()
		if err != nil {
			return fmt.Errorf("获取 stdin pipe 失败: %w", err)
		}
		_ = stdin

		// 若命令需要 sudo 密码，可在这里通过 stdin 写入。
		// 示例：go func() { time.Sleep(500ms); fmt.Fprintln(stdin, "sudo-password") }()
	}

	fmt.Fprintf(os.Stderr, "==> 执行命令: %s\n", opt.command)
	return session.Run(opt.command)
}

func runInteractiveShell(client *ssh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		return fmt.Errorf("请求伪终端失败: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("获取 stdin pipe 失败: %w", err)
	}
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		return fmt.Errorf("启动 shell 失败: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Fprintln(stdin, scanner.Text())
		}
		stdin.Close()
	}()

	return session.Wait()
}

func runStreamingCommand(client *ssh.Client, command string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取 stdout pipe 失败: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("获取 stderr pipe 失败: %w", err)
	}

	if err := session.Start(command); err != nil {
		return fmt.Errorf("启动命令失败: %w", err)
	}

	go streamLines("[out]", stdout)
	go streamLines("[err]", stderr)

	return session.Wait()
}

func streamLines(prefix string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Printf("%s %s\n", prefix, scanner.Text())
	}
}

func main() {
	if err := initConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "初始化配置失败: %v\n", err)
		os.Exit(1)
	}

	opt, configFile := parseFlags()

	config, err := loadConfig(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 加载配置文件失败: %v (将使用命令行参数)\n", err)
	} else {
		if opt.host == "" && config.Global.DefaultHost != "" {
			opt.host = config.Global.DefaultHost
			fmt.Fprintf(os.Stderr, "==> 使用默认主机: %s\n", opt.host)
		}
	}

	if opt.host == "" {
		fmt.Fprintln(os.Stderr, "用法示例:")
		fmt.Fprintln(os.Stderr, "  直接运行: shlink")
		fmt.Fprintln(os.Stderr, "  指定主机: shlink -host production")
		fmt.Fprintln(os.Stderr, "  密码认证: shlink -host 192.168.1.100 -user root -password secret -cmd 'ls -la'")
		fmt.Fprintln(os.Stderr, "  密钥认证: shlink -host 192.168.1.100 -user root -key ~/.ssh/id_rsa -cmd 'ls -la'")
		fmt.Fprintln(os.Stderr, "  跳板机:   shlink -host 10.0.0.5 -user root -key ~/.ssh/id_rsa -jump 192.168.1.1 -cmd 'hostname'")
		fmt.Fprintln(os.Stderr, "  使用配置文件: shlink -host production -cmd 'ls -la'")
		fmt.Fprintln(os.Stderr, "  执行脚本: shlink -host production -script deploy.sh")
		fmt.Fprintln(os.Stderr, "  列出脚本: shlink -host production -list-scripts")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: 加载配置文件失败: %v (将使用命令行参数)\n", err)
	} else {
		if hostConfig, ok := config.Hosts[opt.host]; ok {
			fmt.Fprintf(os.Stderr, "==> 从配置文件加载主机: %s\n", opt.host)
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
			if opt.script == "" && hostConfig.DefaultScript != "" {
				opt.script = hostConfig.DefaultScript
				fmt.Fprintf(os.Stderr, "==> 使用默认脚本: %s\n", opt.script)
			}
			if !opt.usePty {
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

	if opt.listScripts {
		listScripts(opt.scriptDir)
		return
	}

	if opt.script != "" {
		scriptPath := filepath.Join(opt.scriptDir, opt.script)
		scriptContent, err := os.ReadFile(scriptPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取脚本文件失败: %v\n", err)
			os.Exit(1)
		}
		opt.command = "bash -c " + fmt.Sprintf("%q", string(scriptContent))
		fmt.Fprintf(os.Stderr, "==> 加载脚本: %s\n", scriptPath)
	}

	client, err := dial(opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Fprintf(os.Stderr, "==> 已连接到 %s:%s\n", opt.host, opt.port)

	if err := runCommand(client, opt); err != nil {
		fmt.Fprintf(os.Stderr, "命令执行失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "==> 命令执行完成")
}

func initConfig() error {
	dirs := []string{
		"scripts",
		"scripts/production",
		"scripts/staging",
		"scripts/backend",
		"scripts/sudo",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}

	configContent := `hosts:
  production:
    host: 192.168.1.100
    port: 22
    user: root
    password: ""
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/production"
    default_script: "deploy.sh"

  staging:
    host: 192.168.1.101
    port: 22
    user: admin
    password: ""
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/staging"
    default_script: "test.sh"

  backend:
    host: 10.0.0.5
    port: 22
    user: appuser
    key_file: "~/.ssh/id_rsa"
    jump_host: 192.168.1.200
    jump_user: bastion
    jump_key: "~/.ssh/id_rsa"
    script_dir: "scripts/backend"
    default_script: "health.sh"

global:
  timeout: 10
  use_pty: false
  script_dir: "scripts"
  default_host: "production"
`

	if _, err := os.Stat("config.yaml"); os.IsNotExist(err) {
		if err := os.WriteFile("config.yaml", []byte(configContent), 0644); err != nil {
			return fmt.Errorf("创建配置文件失败: %w", err)
		}
		fmt.Fprintln(os.Stderr, "==> 已创建配置文件: config.yaml")
	}

	scripts := map[string]string{
		"scripts/production/deploy.sh": `#!/bin/bash
echo "Deploying to production..."
cd /var/www/app
git pull origin main
docker-compose down
docker-compose up -d
echo "Deployment completed."`,
		"scripts/staging/test.sh": `#!/bin/bash
echo "Running tests on staging..."
cd /var/www/app
npm test
echo "Tests completed."`,
		"scripts/backend/health.sh": `#!/bin/bash
echo "Checking backend health..."
curl -s http://localhost:8080/health
echo ""
echo "Health check completed."`,
		"scripts/sudo/update.sh": `#!/bin/bash
echo "Running system update..."
sudo apt-get update && sudo apt-get upgrade -y
echo "System update completed."`,
	}

	for path, content := range scripts {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(content), 0755); err != nil {
				return fmt.Errorf("创建脚本文件失败 %s: %w", path, err)
			}
			fmt.Fprintln(os.Stderr, "==> 已创建脚本文件:", path)
		}
	}

	return nil
}

func listScripts(scriptDir string) {
	files, err := os.ReadDir(scriptDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取脚本目录失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("可用脚本列表 (%s):\n", scriptDir)
	for _, file := range files {
		if !file.IsDir() {
			fmt.Printf("  - %s\n", file.Name())
		}
	}
}