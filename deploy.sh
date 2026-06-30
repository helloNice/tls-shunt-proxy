#!/bin/bash

# 部署脚本 - 在 sdkdata 服务器上执行

echo "=== 部署 tls-shunt-proxy 到 sdkdata 服务器 ==="
echo ""

# 停止旧服务
echo "1. 停止旧服务..."
pkill -f tls-shunt-proxy || true
sleep 2

# 备份旧版本
echo "2. 备份旧版本..."
if [ -f /root/tls-shunt-proxy/tls-shunt-proxy ]; then
    cp /root/tls-shunt-proxy/tls-shunt-proxy /root/tls-shunt-proxy/tls-shunt-proxy.backup.$(date +%Y%m%d_%H%M%S)
fi

# 等待二进制文件上传
echo "3. 等待二进制文件上传..."
echo "请将本地的 tls-shunt-proxy-linux-amd64 文件上传到服务器的 /root/tls-shunt-proxy/ 目录"
echo "上传完成后，按回车键继续..."
read

# 重命名二进制文件
echo "4. 重命名二进制文件..."
cd /root/tls-shunt-proxy
mv tls-shunt-proxy-linux-amd64 tls-shunt-proxy
chmod +x tls-shunt-proxy

# 创建配置文件（如果不存在）
echo "5. 检查配置文件..."
if [ ! -f /root/tls-shunt-proxy/config.yaml ]; then
    echo "创建默认配置文件..."
    cat > /root/tls-shunt-proxy/config.yaml << 'EOF'
listen: 0.0.0.0:443
redirecthttps: 0.0.0.0:80
inboundbuffersize: 4
outboundbuffersize: 32
fallback: 127.0.0.1:8443
webui_listen: 127.0.0.1:8080

vhosts:
  - name: example.com
    tlsoffloading: true
    managedcert: false
    cert: /path/to/cert.pem
    key: /path/to/key.pem
    keytype: p256
    alpn: http/1.1,h2
    protocols: tls12,tls13
    http:
      handler: proxyPass
      args: 127.0.0.1:8080
EOF
fi

# 启动服务
echo "6. 启动服务..."
nohup /root/tls-shunt-proxy/tls-shunt-proxy -config /root/tls-shunt-proxy/config.yaml > /root/tls-shunt-proxy/nohup.out 2>&1 &

sleep 3

# 检查服务状态
echo "7. 检查服务状态..."
if pgrep -f tls-shunt-proxy > /dev/null; then
    echo "✓ 服务启动成功"
    echo ""
    echo "服务信息："
    echo "  进程 ID: $(pgrep -f tls-shunt-proxy)"
    echo "  Web UI: http://127.0.0.1:8080 (默认账号: admin/admin)"
    echo "  监听端口: 443 (HTTPS), 80 (HTTP 重定向)"
    echo ""
    echo "查看日志: tail -f /root/tls-shunt-proxy/nohup.out"
else
    echo "✗ 服务启动失败"
    echo "查看日志: cat /root/tls-shunt-proxy/nohup.out"
fi

echo ""
echo "=== 部署完成 ==="