#!/bin/bash
# tls-shunt-proxy 内存监控脚本
# 每5秒记录一次内存占用到文件

LOG_FILE="/tmp/tls-shunt-proxy-memory.log"
PID_FILE="/tmp/tls-shunt-proxy-monitor.pid"

# 检查是否已经在运行
if [ -f "$PID_FILE" ]; then
    OLD_PID=$(cat "$PID_FILE")
    if ps -p "$OLD_PID" > /dev/null 2>&1; then
        echo "监控进程已在运行 (PID: $OLD_PID)"
        echo "如需停止，请执行: kill $OLD_PID"
        exit 1
    else
        rm -f "$PID_FILE"
    fi
fi

# 记录当前进程 PID
echo $$ > "$PID_FILE"

# 写入日志头
echo "=== tls-shunt-proxy 内存监控开始 ===" > "$LOG_FILE"
echo "开始时间: $(date '+%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
echo "监控间隔: 5 秒" >> "$LOG_FILE"
echo "======================================" >> "$LOG_FILE"
echo "" >> "$LOG_FILE"

# 监控循环
while true; do
    TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')
    PID=$(pgrep -f "/opt/tls-shunt-proxy/tls-shunt-proxy" | head -1)
    
    if [ -n "$PID" ]; then
        # 获取内存信息
        MEM_INFO=$(cat /proc/$PID/status 2>/dev/null | grep -E "VmRSS|VmSize|VmPeak" | awk '{print $2, $3}')
        
        # 使用 top 获取更准确的 RES 内存
        TOP_INFO=$(top -b -n 1 -p $PID 2>/dev/null | grep $PID | awk '{print $6, $8, $9}')
        
        echo "[$TIMESTAMP] PID: $PID | VmRSS/VmSize: $MEM_INFO | RES/CPU/TIME: $TOP_INFO" >> "$LOG_FILE"
    else
        echo "[$TIMESTAMP] WARNING: tls-shunt-proxy 进程未找到" >> "$LOG_FILE"
    fi
    
    sleep 5
done