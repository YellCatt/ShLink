package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sagikazarmark/slog-shim"
)

func saveReport(host, script, output string) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}

	reportDir := filepath.Join("report", host)
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		slog.Error("创建报告目录失败", "error", err, "directory", reportDir)
		return
	}

	timestamp := time.Now().In(loc).Format("20060102_150405")
	scriptName := strings.TrimSuffix(script, ".sh")
	reportFile := filepath.Join(reportDir, fmt.Sprintf("%s_%s.log", scriptName, timestamp))

	lines := strings.Split(output, "\n")
	var formattedOutput strings.Builder
	for _, line := range lines {
		if line != "" {
			formattedOutput.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().In(loc).Format("2006-01-02 15:04:05"), line))
		}
	}

	if err := os.WriteFile(reportFile, []byte(formattedOutput.String()), 0644); err != nil {
		slog.Error("保存报告失败", "error", err, "file", reportFile)
		return
	}

	slog.Info("报告已保存", "file", reportFile)
}

func saveSummary(host, script, status, errorMsg string) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}

	summaryFile := filepath.Join("report", "summary.log")
	var entry string
	if errorMsg != "" {
		entry = fmt.Sprintf("[%s] %s | %s | %s | %s\n",
			time.Now().In(loc).Format("2006-01-02 15:04:05"),
			host,
			script,
			status,
			errorMsg)
	} else {
		entry = fmt.Sprintf("[%s] %s | %s | %s\n",
			time.Now().In(loc).Format("2006-01-02 15:04:05"),
			host,
			script,
			status)
	}

	f, err := os.OpenFile(summaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("保存摘要失败", "error", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		slog.Error("写入摘要失败", "error", err)
	}
}

func saveFailedMarker(host string) {
	failedDir := filepath.Join("report", "failed")
	if err := os.MkdirAll(failedDir, 0755); err != nil {
		slog.Error("创建失败目录失败", "error", err)
		return
	}

	markerFile := filepath.Join(failedDir, host)
	if err := os.WriteFile(markerFile, []byte(""), 0644); err != nil {
		slog.Error("创建失败标记失败", "error", err)
	}
}

func listFailed() {
	failedDir := filepath.Join("report", "failed")
	files, err := os.ReadDir(failedDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("没有失败的环境")
			return
		}
		slog.Error("读取失败目录失败", "error", err)
		return
	}

	if len(files) == 0 {
		slog.Info("没有失败的环境")
		return
	}

	slog.Info("失败的环境列表:")
	for _, file := range files {
		if !file.IsDir() {
			slog.Warn("失败环境", "host", file.Name())
		}
	}
}

func listScripts(scriptDir string) {
	files, err := os.ReadDir(scriptDir)
	if err != nil {
		slog.Error("读取脚本目录失败", "error", err)
		os.Exit(1)
	}

	slog.Info("可用脚本列表", "directory", scriptDir)
	for _, file := range files {
		if !file.IsDir() {
			slog.Info("脚本文件", "name", file.Name())
		}
	}
}