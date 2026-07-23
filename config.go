package main

import (
	"fmt"
	"os"

	"github.com/sagikazarmark/slog-shim"
	"gopkg.in/yaml.v3"
)

type HostConfig struct {
	Host        string `yaml:"host"`
	Port        string `yaml:"port"`
	User        string `yaml:"user"`
	Password    string `yaml:"password"`
	KeyFile     string `yaml:"key_file"`
	JumpHost    string `yaml:"jump_host"`
	JumpUser    string `yaml:"jump_user"`
	JumpKey     string `yaml:"jump_key"`
	JumpPasswd  string `yaml:"jump_passwd"`
	ScriptDir   string `yaml:"script_dir"`
}

type GlobalConfig struct {
	Timeout     int  `yaml:"timeout"`
	UsePty      bool `yaml:"use_pty"`
	ScriptDir   string `yaml:"script_dir"`
	DefaultHost string `yaml:"default_host"`
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
		slog.Info("已创建配置文件", "file", "config.yaml")
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
			slog.Info("已创建脚本文件", "file", path)
		}
	}

	return nil
}