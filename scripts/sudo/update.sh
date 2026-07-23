#!/bin/bash
# 系统更新脚本（需要 sudo）
echo "=== 系统更新 ==="
echo "更新包列表..."
sudo apt update -y
echo "升级已安装包..."
sudo apt upgrade -y
echo "清理旧包..."
sudo apt autoremove -y
echo "=== 更新完成 ==="