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
		"scripts/uat",
		"scripts/backend",
		"scripts/database",
		"scripts/frontend",
		"scripts/monitor",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}

	configContent := `hosts:
  production:
    host: 172.16.4.225
    port: 22
    user: root
    password: ""
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/production"

  production-backup:
    host: 172.16.4.226
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

  uat:
    host: 172.16.4.235
    port: 22
    user: tester
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/uat"

  backend-api:
    host: 10.0.1.5
    port: 22
    user: appuser
    key_file: "~/.ssh/id_rsa"
    jump_host: 172.16.4.200
    jump_user: bastion
    jump_key: "~/.ssh/id_rsa"
    script_dir: "scripts/backend"

  backend-db:
    host: 10.0.1.6
    port: 22
    user: dbuser
    key_file: "~/.ssh/id_rsa"
    jump_host: 172.16.4.200
    jump_user: bastion
    jump_key: "~/.ssh/id_rsa"
    script_dir: "scripts/database"

  frontend:
    host: 172.16.4.240
    port: 22
    user: webuser
    password: ""
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/frontend"

  monitor:
    host: 172.16.4.250
    port: 22
    user: monitor
    key_file: "~/.ssh/id_rsa"
    script_dir: "scripts/monitor"

global:
  timeout: 10
  use_pty: false
  script_dir: "scripts"
  default_host: "production"
  parallel: false
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
		"scripts/production/health.sh": `#!/bin/bash
echo "Checking production health..."
curl -s http://localhost/health
echo ""
echo "Production health check completed."`,
		"scripts/staging/test.sh": `#!/bin/bash
echo "Running tests on staging..."
cd /var/www/app
npm test
echo "Tests completed."`,
		"scripts/staging/build.sh": `#!/bin/bash
echo "Building on staging..."
npm install
npm run build
echo "Build completed."`,
		"scripts/uat/run-tests.sh": `#!/bin/bash
echo "Running UAT tests..."
cd /var/www/app
npm run test:uat
echo "UAT tests completed."`,
		"scripts/backend/health.sh": `#!/bin/bash
echo "Checking backend health..."
curl -s http://localhost:8080/health
echo ""
echo "Backend health check completed."`,
		"scripts/backend/restart.sh": `#!/bin/bash
echo "Restarting backend service..."
systemctl restart backend
echo "Backend restart completed."`,
		"scripts/database/backup.sh": `#!/bin/bash
echo "Starting database backup..."
mysqldump -u root -p mydb > /backup/db_backup_$(date +%Y%m%d).sql
echo "Database backup completed."`,
		"scripts/database/check.sh": `#!/bin/bash
echo "Checking database status..."
mysqladmin ping -u root -p
echo "Database check completed."`,
		"scripts/frontend/deploy.sh": `#!/bin/bash
echo "Deploying frontend..."
cd /var/www/frontend
git pull origin main
npm install
npm run build
echo "Frontend deployment completed."`,
		"scripts/monitor/check-services.sh": `#!/bin/bash
echo "Checking monitor services..."
systemctl status prometheus
systemctl status grafana
echo "Monitor services check completed."`,
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