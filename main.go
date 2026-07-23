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

	"golang.org/x/crypto/ssh"
)

type options struct {
	host       string
	port       string
	user       string
	password   string
	keyFile    string
	command    string
	usePty     bool
	jumpHost   string
	jumpUser   string
	jumpKey    string
	jumpPasswd string
}

func parseFlags() options {
	var opt options
	flag.StringVar(&opt.host, "host", "", "远程主机地址 (必填)")
	flag.StringVar(&opt.port, "port", "22", "远程主机端口")
	flag.StringVar(&opt.user, "user", "root", "登录用户名")
	flag.StringVar(&opt.password, "password", "", "登录密码")
	flag.StringVar(&opt.keyFile, "key", "", "私钥文件路径 (例如 ~/.ssh/id_rsa)")
	flag.StringVar(&opt.command, "cmd", "", "要执行的命令 (必填)")
	flag.BoolVar(&opt.usePty, "pty", false, "是否申请伪终端 (sudo 等交互场景)")
	flag.StringVar(&opt.jumpHost, "jump", "", "跳板机地址 host:port")
	flag.StringVar(&opt.jumpUser, "jump-user", "", "跳板机用户名，默认与 user 相同")
	flag.StringVar(&opt.jumpKey, "jump-key", "", "跳板机私钥")
	flag.StringVar(&opt.jumpPasswd, "jump-pass", "", "跳板机密码")
	flag.Parse()
	return opt
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
	opt := parseFlags()

	if opt.host == "" || opt.command == "" {
		fmt.Fprintln(os.Stderr, "用法示例:")
		fmt.Fprintln(os.Stderr, "  密码认证: ShLink -host 192.168.1.100 -user root -password secret -cmd 'ls -la'")
		fmt.Fprintln(os.Stderr, "  密钥认证: ShLink -host 192.168.1.100 -user root -key ~/.ssh/id_rsa -cmd 'ls -la'")
		fmt.Fprintln(os.Stderr, "  跳板机:   ShLink -host 10.0.0.5 -user root -key ~/.ssh/id_rsa -jump 192.168.1.1 -cmd 'hostname'")
		flag.PrintDefaults()
		os.Exit(1)
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
