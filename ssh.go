package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
	}

	return session.Run(opt.command)
}

func runScript(client *ssh.Client, scriptContent []byte) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建会话失败: %w", err)
	}
	defer session.Close()

	var output strings.Builder
	session.Stdout = io.MultiWriter(os.Stdout, &output)
	session.Stderr = io.MultiWriter(os.Stderr, &output)

	stdin, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("获取 stdin pipe 失败: %w", err)
	}

	shells := []string{"bash", "sh"}
	var lastErr error

	for _, shell := range shells {
		if err := session.Start(shell); err != nil {
			lastErr = fmt.Errorf("启动 %s 失败: %w", shell, err)
			continue
		}

		if _, err := stdin.Write(scriptContent); err != nil {
			return "", fmt.Errorf("写入脚本内容失败: %w", err)
		}
		stdin.Close()

		if err := session.Wait(); err != nil {
			return output.String(), err
		}

		return output.String(), nil
	}

	return "", lastErr
}