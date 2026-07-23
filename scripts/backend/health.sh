#!/bin/bash
# 后端服务健康检查脚本
echo "=== 健康检查 ==="
echo "检查服务状态..."
systemctl status nginx 2>/dev/null || echo "nginx: 未运行"
systemctl status mysql 2>/dev/null || echo "mysql: 未运行"
echo "检查端口..."
netstat -tlnp 2>/dev/null | grep -E ":80|:443|:3306"
echo "=== 检查完成 ==="