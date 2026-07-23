#!/bin/bash
# 测试环境测试脚本
echo "=== 测试环境 ==="
echo "CPU 信息:"
cat /proc/cpuinfo | grep "model name" | head -1
echo "内存信息:"
free -h
echo "磁盘信息:"
df -h
echo "=== 测试完成 ==="