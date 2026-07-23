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
	Tags        []string `yaml:"tags"`
}

type EnvGroup struct {
	Name        string   `yaml:"name"`
	Pattern     string   `yaml:"pattern"`
	Hosts       []string `yaml:"hosts"`
	ScriptDir   string   `yaml:"script_dir"`
}

type Config struct {
	Hosts        map[string]HostConfig `yaml:"hosts"`
	EnvGroups    []EnvGroup           `yaml:"env_groups"`
	Global       GlobalConfig         `yaml:"global"`
}

type GlobalConfig struct {
	Timeout        int  `yaml:"timeout"`
	UsePty         bool `yaml:"use_pty"`
	ScriptDir      string `yaml:"script_dir"`
	DefaultHost    string `yaml:"default_host"`
	Parallel       bool `yaml:"parallel"`
	ParallelWorkers int  `yaml:"parallel_workers"`
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
	if err := os.MkdirAll("scripts", 0755); err != nil {
		return fmt.Errorf("创建脚本目录失败: %w", err)
	}

	configContent := `hosts:
  production:
    host: 172.16.4.225
    port: 22
    user: root
    password: ""
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/production"

  staging:
    host: 172.16.4.230
    port: 22
    user: admin
    password: ""
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/staging"

global:
  timeout: 10
  use_pty: false
  script_dir: "scripts"
  default_host: "production"
  parallel: true
  parallel_workers: 10
`

	if _, err := os.Stat("config.yaml"); os.IsNotExist(err) {
		if err := os.WriteFile("config.yaml", []byte(configContent), 0644); err != nil {
			return fmt.Errorf("创建配置文件失败: %w", err)
		}
		slog.Info("已创建配置文件", "file", "config.yaml")
	}

	return nil
}