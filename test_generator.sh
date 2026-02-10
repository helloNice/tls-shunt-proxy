#!/bin/bash

# 测试配置生成器的脚本

echo "=== 测试配置生成器 ==="
echo ""

# 创建测试数据
TEST_DATA='{
  "domain": "test.example.com",
  "cloudflare_token": "test-token-12345",
  "services": [
    {
      "subdomain": "web",
      "type": "http",
      "backend_port": 8080
    },
    {
      "subdomain": "api",
      "type": "tcp",
      "backend_port": 8443
    },
    {
      "subdomain": "ws",
      "type": "websocket",
      "backend_port": 9090
    }
  ]}'

# 启动 tls-shunt-proxy（后台运行）
echo "启动 tls-shunt-proxy..."
./tls-shunt-proxy -config config.yaml &
PROXY_PID=$!
sleep 2

echo ""
echo "测试数据："
echo "$TEST_DATA" | jq .
echo ""

# 测试生成配置 API
echo "=== 测试 /api/generate-strategy-config 接口 ==="
RESPONSE=$(curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Basic YWRtaW46YWRtaW4=" \
  -d "$TEST_DATA" \
  http://127.0.0.1:8080/api/generate-strategy-config)

echo "响应："
echo "$RESPONSE" | jq .

# 提取配置内容
CONFIG=$(echo "$RESPONSE" | jq -r '.data.config')

if [ "$CONFIG" != "null" ] && [ -n "$CONFIG" ]; then
    echo ""
    echo "=== 生成的配置 ==="
    echo "$CONFIG"

    # 保存生成的配置到文件
    echo "$CONFIG" > /tmp/test_generated_config.yaml
    echo ""
    echo "配置已保存到 /tmp/test_generated_config.yaml"

    # 测试保存配置 API
    echo ""
    echo "=== 测试 /api/save-strategy-config 接口 ==="
    SAVE_DATA="{\"config\": $(echo "$CONFIG" | jq -Rs .)}"
    SAVE_RESPONSE=$(curl -s -X POST \
      -H "Content-Type: application/json" \
      -H "Authorization: Basic YWRtaW46YWRtaW4=" \
      -d "$SAVE_DATA" \
      http://127.0.0.1:8080/api/save-strategy-config)

    echo "保存响应："
    echo "$SAVE_RESPONSE" | jq .
else
    echo "错误：未能生成配置"
fi

# 清理
echo ""
echo "清理进程..."
kill $PROXY_PID 2>/dev/null

echo ""
echo "测试完成！"