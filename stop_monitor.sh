#!/bin/bash
# 停止 tls-shunt-proxy 内存监控脚本

PID_FILE="/tmp/tls-shunt-proxy-monitor.pid"

if [ ! -f "$PID_FILE" ]; then
    echo "未找到监控进程 PID 文件"
    exit 1
fi

PID=$(cat "$PID_FILE")

if ps -p "$PID" > /dev/null 2>&1; then
    kill $PID
    rm -f "$PID_FILE"
    echo "监控进程已停止 (PID: $PID)"
    echo "日志文件: /tmp/tls-shunt-proxy-memory.log"
else
    echo "监控进程不存在 (PID: $PID)"
    rm -f "$PID_FILE"
fi