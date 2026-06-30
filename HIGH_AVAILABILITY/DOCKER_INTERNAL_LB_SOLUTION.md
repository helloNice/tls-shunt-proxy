# Docker 容器化 + 内部负载均衡方案

## 概述

本文档详细介绍了如何使用 Docker 容器化部署 tls-shunt-proxy，并通过内部负载均衡器（Nginx）实现高可用方案。

## 架构设计

```
                    ┌─────────────────────────────────┐
                    │        外部请求 (443)            │
                    └────────────┬────────────────────┘
                                 │
                    ┌────────────▼────────────────────┐
                    │   Nginx 负载均衡器 (容器)         │
                    │   监听: 0.0.0.0:443               │
                    │   监听: 0.0.0.0:80                │
                    └────────────┬────────────────────┘
                                 │
         ┌───────────────────────┼───────────────────────┐
         │                       │                       │
    ┌────▼────┐            ┌────▼────┐            ┌────▼────┐
    │ tls-shunt│            │ tls-shunt│            │ tls-shunt│
    │ -proxy  │            │ -proxy  │            │ -proxy  │
    │ 容器 1  │            │ 容器 2  │            │ 容器 N  │
    │ :8443   │            │ :8444   │            │ :8445   │
    └────┬────┘            └────┬────┘            └────┬────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌────────────▼────────────────────┐
                    │      后端服务                    │
                    │  192.168.7.128:8080              │
                    └─────────────────────────────────┘
```

## 部署场景

**目标域名**: `web.xiongfei@test.com`
**后端服务**: `192.168.7.128:8080`
**部署方式**: Docker 容器化
**负载均衡**: Nginx 内部负载均衡

---

## 项目结构

```
tls-shunt-proxy/
├── docker/
│   ├── nginx/
│   │   └── nginx.conf           # Nginx 负载均衡配置
│   ├── tls-shunt-proxy/
│   │   ├── Dockerfile           # tls-shunt-proxy 镜像
│   │   └── entrypoint.sh        # 容器启动脚本
│   └── docker-compose.yml       # Docker Compose 配置
├── config/
│   └── config.yaml              # tls-shunt-proxy 配置
└── certificates/
    └── web.xiongfei@test.com/   # 证书目录
        ├── fullchain.pem
        └── privkey.pem
```

---

## 第一步：准备目录结构

```bash
# 在项目根目录创建必要的目录
mkdir -p docker/nginx
mkdir -p docker/tls-shunt-proxy
mkdir -p certificates/web.xiongfei@test.com
mkdir -p config
```

---

## 第二步：创建 Nginx 配置文件

创建 `docker/nginx/nginx.conf`：

```nginx
user nginx;
worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 1024;
    use epoll;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';

    access_log /var/log/nginx/access.log main;

    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;

    # 健康检查端点
    server {
        listen 8080;
        server_name localhost;

        location /health {
            access_log off;
            return 200 "healthy\n";
            add_header Content-Type text/plain;
        }
    }

    # HTTPS 负载均衡
    upstream tls_shunt_proxy_backend {
        least_conn;
        server tls-shunt-proxy-1:8443 max_fails=3 fail_timeout=30s;
        server tls-shunt-proxy-2:8444 max_fails=3 fail_timeout=30s;
        server tls-shunt-proxy-3:8445 max_fails=3 fail_timeout=30s;

        keepalive 32;
    }

    # HTTP 重定向到 HTTPS
    server {
        listen 80;
        server_name web.xiongfei@test.com;

        location / {
            return 301 https://$host$request_uri;
        }

        # Let's Encrypt ACME 验证
        location /.well-known/acme-challenge/ {
            proxy_pass http://tls_shunt_proxy_backend;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }
    }

    # HTTPS 代理
    server {
        listen 443 ssl http2;
        server_name web.xiongfei@test.com;

        # SSL 证书配置
        ssl_certificate /etc/nginx/ssl/web.xiongfei@test.com/fullchain.pem;
        ssl_certificate_key /etc/nginx/ssl/web.xiongfei@test.com/privkey.pem;

        # SSL 配置
        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384';
        ssl_prefer_server_ciphers off;

        # SSL 会话缓存
        ssl_session_cache shared:SSL:10m;
        ssl_session_timeout 10m;

        # SSL HSTS
        add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

        # 代理到 tls-shunt-proxy 后端
        location / {
            proxy_pass https://tls_shunt_proxy_backend;

            # 代理头设置
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header X-Forwarded-Host $host;
            proxy_set_header X-Forwarded-Port $server_port;

            # SSL 代理配置
            proxy_ssl_verify off;
            proxy_ssl_session_reuse on;

            # 超时配置
            proxy_connect_timeout 60s;
            proxy_send_timeout 60s;
            proxy_read_timeout 60s;

            # 缓冲区配置
            proxy_buffering off;
            proxy_request_buffering off;

            # WebSocket 支持
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
        }

        # 健康检查
        location /nginx-health {
            access_log off;
            return 200 "nginx is healthy\n";
            add_header Content-Type text/plain;
        }
    }
}
```

---

## 第三步：创建 tls-shunt-proxy 配置文件

创建 `config/config.yaml`：

```yaml
# 监听地址（容器内部端口）
listen: 0.0.0.0:8443

# HTTPS 重定向监听地址
redirecthttps: 0.0.0.0:8081

# 入站缓冲区大小（KB）
inboundbuffersize: 4

# 出站缓冲区大小（KB）
outboundbuffersize: 32

# 未知 SNI 的回落地址
fallback: 127.0.0.1:8080

# 虚拟主机配置
vhosts:
  # web.xiongfei@test.com 域名配置
  - name: web.xiongfei@test.com

    # TLS 卸载：true 表示解密 TLS，识别协议类型
    tlsoffloading: true

    # 证书管理：false 表示使用本地证书
    managedcert: false

    # 密钥类型
    keytype: p256

    # 证书路径（容器内路径）
    cert: /certificates/web.xiongfei@test.com/fullchain.pem
    key: /certificates/web.xiongfei@test.com/privkey.pem

    # ALPN 协议
    alpn: h2,http/1.1

    # TLS 协议版本
    protocols: tls12,tls13

    # HTTP 流量处理
    http:
      paths:
        # WebSocket 路径
        - path: /ws/
          handler: proxyPass
          args: 192.168.7.128:8080

        # API 路径
        - path: /api/
          handler: proxyPass
          args: 192.168.7.128:8080

        # 默认路径
        - path: "*"
          handler: proxyPass
          args: 192.168.7.128:8080

    # HTTP/2 流量处理
    http2:
      - path: /
        handler: proxyPass
        args: 192.168.7.128:8080

    # Trojan 流量处理（如果需要）
    trojan:
      handler: noop

    # 默认流量处理
    default:
      handler: proxyPass
      args: 192.168.7.128:8080
```

---

## 第四步：创建 tls-shunt-proxy Dockerfile

创建 `docker/tls-shunt-proxy/Dockerfile`：

```dockerfile
# 构建阶段
FROM golang:1.22-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /build

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o tls-shunt-proxy \
    main.go

# 运行阶段
FROM alpine:latest

# 安装必要的运行时依赖
RUN apk add --no-cache ca-certificates tzdata curl

# 创建非 root 用户
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# 设置工作目录
WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /build/tls-shunt-proxy .

# 创建必要的目录
RUN mkdir -p /certificates /config && \
    chown -R appuser:appuser /app /certificates /config

# 切换到非 root 用户
USER appuser

# 暴露端口
EXPOSE 8443 8081 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# 启动命令
CMD ["./tls-shunt-proxy", "-config", "/config/config.yaml"]
```

---

## 第五步：创建 Docker Compose 配置

创建 `docker/docker-compose.yml`：

```yaml
version: '3.8'

services:
  # Nginx 负载均衡器
  nginx:
    image: nginx:alpine
    container_name: nginx-lb
    ports:
      - "80:80"
      - "443:443"
      - "8080:8080"
    volumes:
      - ./nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - ../certificates:/etc/nginx/ssl:ro
      - nginx-logs:/var/log/nginx
    depends_on:
      - tls-shunt-proxy-1
      - tls-shunt-proxy-2
      - tls-shunt-proxy-3
    networks:
      - tls-shunt-network
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

  # tls-shunt-proxy 实例 1
  tls-shunt-proxy-1:
    build:
      context: ./tls-shunt-proxy
      dockerfile: Dockerfile
    container_name: tls-shunt-proxy-1
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ../config:/config:ro
      - ../certificates:/certificates:ro
    ports:
      - "8443:8443"
      - "8081:8081"
    networks:
      - tls-shunt-network
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

  # tls-shunt-proxy 实例 2
  tls-shunt-proxy-2:
    build:
      context: ./tls-shunt-proxy
      dockerfile: Dockerfile
    container_name: tls-shunt-proxy-2
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ../config:/config:ro
      - ../certificates:/certificates:ro
    ports:
      - "8444:8443"
      - "8082:8081"
    networks:
      - tls-shunt-network
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

  # tls-shunt-proxy 实例 3
  tls-shunt-proxy-3:
    build:
      context: ./tls-shunt-proxy
      dockerfile: Dockerfile
    container_name: tls-shunt-proxy-3
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ../config:/config:ro
      - ../certificates:/certificates:ro
    ports:
      - "8445:8443"
      - "8083:8081"
    networks:
      - tls-shunt-network
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

# 网络配置
networks:
  tls-shunt-network:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/16

# 卷配置
volumes:
  nginx-logs:
    driver: local
```

---

## 第六步：准备证书

### 使用 Let's Encrypt 获取证书

```bash
# 安装 certbot
sudo apt-get install certbot -y  # Ubuntu/Debian
# 或
sudo yum install certbot -y      # CentOS/RHEL

# 获取证书（确保域名已解析到服务器 IP）
sudo certbot certonly --standalone \
  -d web.xiongfei@test.com \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive

# 证书位置
# /etc/letsencrypt/live/web.xiongfei@test.com/fullchain.pem
# /etc/letsencrypt/live/web.xiongfei@test.com/privkey.pem

# 复制证书到项目目录
sudo mkdir -p certificates/web.xiongfei@test.com
sudo cp /etc/letsencrypt/live/web.xiongfei@test.com/fullchain.pem certificates/web.xiongfei@test.com/
sudo cp /etc/letsencrypt/live/web.xiongfei@test.com/privkey.pem certificates/web.xiongfei@test.com/
sudo chmod 644 certificates/web.xiongfei@test.com/*.pem
```

### 使用自签名证书（测试用）

```bash
# 生成自签名证书
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout certificates/web.xiongfei@test.com/privkey.pem \
  -out certificates/web.xiongfei@test.com/fullchain.pem \
  -subj "/CN=web.xiongfei@test.com/O=Test/C=CN"
```

---

## 第七步：构建和部署

### 1. 构建镜像

```bash
# 进入 docker 目录
cd docker

# 构建并启动所有服务
docker-compose build

# 或者仅构建 tls-shunt-proxy 镜像
docker-compose build tls-shunt-proxy-1 tls-shunt-proxy-2 tls-shunt-proxy-3
```

### 2. 启动服务

```bash
# 启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f

# 查看特定服务日志
docker-compose logs -f nginx
docker-compose logs -f tls-shunt-proxy-1
```

### 3. 验证部署

```bash
# 检查 Nginx 健康状态
curl http://localhost:8080/health

# 检查 tls-shunt-proxy 健康状态
curl http://localhost:8443/health
curl http://localhost:8444/health
curl http://localhost:8445/health

# 测试 HTTPS 连接
curl -k https://web.xiongfei@test.com

# 测试 WebSocket 连接
wscat -c wss://web.xiongfei@test.com/ws

# 查看 Docker 网络
docker network inspect docker_tls-shunt-network
```

---

## 第八步：配置证书自动续期

### 使用 certbot-auto-renew

```bash
# 创建续期脚本
cat > /usr/local/bin/renew-cert.sh << 'EOF'
#!/bin/bash

# 续期证书
sudo certbot renew --quiet

# 复制新证书到项目目录
sudo cp /etc/letsencrypt/live/web.xiongfei@test.com/fullchain.pem /path/to/tls-shunt-proxy/certificates/web.xiongfei@test.com/
sudo cp /etc/letsencrypt/live/web.xiongfei@test.com/privkey.pem /path/to/tls-shunt-proxy/certificates/web.xiongfei@test.com/

# 重启 Nginx 容器（重新加载证书）
cd /path/to/tls-shunt-proxy/docker
docker-compose exec nginx nginx -s reload

# 重启 tls-shunt-proxy 容器（重新加载证书）
docker-compose restart tls-shunt-proxy-1 tls-shunt-proxy-2 tls-shunt-proxy-3
EOF

chmod +x /usr/local/bin/renew-cert.sh

# 添加定时任务（每天 2:00 执行）
(crontab -l 2>/dev/null; echo "0 2 * * * /usr/local/bin/renew-cert.sh >> /var/log/cert-renew.log 2>&1") | crontab -
```

---

## 第九步：监控和日志

### 1. 日志管理

```bash
# 查看所有容器日志
docker-compose logs -f

# 查看 Nginx 访问日志
docker-compose exec nginx tail -f /var/log/nginx/access.log

# 查看 Nginx 错误日志
docker-compose exec nginx tail -f /var/log/nginx/error.log

# 查看 tls-shunt-proxy 日志
docker-compose logs -f tls-shunt-proxy-1
```

### 2. 监控指标

```bash
# 容器资源使用情况
docker stats

# 查看容器详情
docker inspect nginx-lb
docker inspect tls-shunt-proxy-1

# 查看网络连接
docker-compose exec nginx netstat -tuln
docker-compose exec tls-shunt-proxy-1 netstat -tuln
```

### 3. 健康检查

```bash
# 检查所有容器健康状态
docker-compose ps

# 查看健康检查详情
docker inspect --format='{{json .State.Health}}' nginx-lb | jq
```

---

## 第十步：扩展和优化

### 1. 水平扩展

```yaml
# 在 docker-compose.yml 中添加更多实例
tls-shunt-proxy-4:
  build:
    context: ./tls-shunt-proxy
    dockerfile: Dockerfile
  container_name: tls-shunt-proxy-4
  environment:
    - TZ=Asia/Shanghai
  volumes:
    - ../config:/config:ro
    - ../certificates:/certificates:ro
  ports:
    - "8446:8443"
    - "8084:8081"
  networks:
    - tls-shunt-network
  restart: unless-stopped
```

### 2. 负载均衡策略优化

在 `nginx.conf` 中修改 upstream 配置：

```nginx
upstream tls_shunt_proxy_backend {
    # 使用 IP Hash 保持会话
    ip_hash;

    # 或者使用最少连接
    least_conn;

    server tls-shunt-proxy-1:8443 weight=3 max_fails=3 fail_timeout=30s;
    server tls-shunt-proxy-2:8444 weight=2 max_fails=3 fail_timeout=30s;
    server tls-shunt-proxy-3:8445 weight=1 max_fails=3 fail_timeout=30s;

    # 备用服务器
    # server tls-shunt-proxy-4:8446 backup;

    keepalive 32;
}
```

### 3. 性能优化

```nginx
# 在 nginx.conf 中添加
http {
    # 工作进程数
    worker_processes auto;

    # 每个工作进程的最大连接数
    events {
        worker_connections 4096;
        use epoll;
        multi_accept on;
    }

    # 开启 Gzip 压缩
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml;

    # 缓存配置
    proxy_cache_path /var/cache/nginx levels=1:2 keys_zone=my_cache:10m max_size=1g inactive=60m;

    # 限流配置
    limit_req_zone $binary_remote_addr zone=api_limit:10m rate=10r/s;
}
```

---

## 故障排查

### 1. 容器无法启动

```bash
# 查看容器日志
docker-compose logs <container_name>

# 检查端口占用
sudo netstat -tuln | grep -E ':(80|443|8443|8444|8445)'

# 检查配置文件
docker-compose config
```

### 2. 无法访问服务

```bash
# 检查防火墙
sudo firewall-cmd --list-all

# 检查 DNS 解析
nslookup web.xiongfei@test.com

# 检查网络连通性
ping 192.168.7.128
telnet 192.168.7.128 8080
```

### 3. 证书问题

```bash
# 检查证书有效期
openssl x509 -in certificates/web.xiongfei@test.com/fullchain.pem -noout -dates

# 检查证书和密钥是否匹配
openssl x509 -noout -modulus -in certificates/web.xiongfei@test.com/fullchain.pem | openssl md5
openssl rsa -noout -modulus -in certificates/web.xiongfei@test.com/privkey.pem | openssl md5

# 测试 SSL 连接
openssl s_client -connect web.xiongfei@test.com:443 -servername web.xiongfei@test.com
```

---

## 生产环境建议

### 1. 使用生产级证书

```bash
# 使用 Let's Encrypt 证书
sudo certbot certonly --standalone \
  -d web.xiongfei@test.com \
  --email your-email@example.com \
  --agree-tos \
  --force-renewal

# 或使用商业证书
# 购买证书后上传到服务器
```

### 2. 安全加固

```nginx
# 在 nginx.conf 中添加安全头
add_header X-Frame-Options "SAMEORIGIN" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-XSS-Protection "1; mode=block" always;
add_header Referrer-Policy "no-referrer-when-downgrade" always;

# 限制请求方法
if ($request_method !~ ^(GET|HEAD|POST|PUT|DELETE|OPTIONS)$ ) {
    return 405;
}
```

### 3. 日志轮转

```bash
# 配置日志轮转
sudo tee /etc/logrotate.d/docker-nginx << 'EOF'
/var/lib/docker/volumes/docker_nginx-logs/_data/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0640 nginx nginx
    sharedscripts
    postrotate
        docker-compose exec nginx nginx -s reopen > /dev/null 2>&1 || true
    endscript
}
EOF
```

### 4. 备份策略

```bash
# 备份脚本
cat > /usr/local/bin/backup-tls-shunt.sh << 'EOF'
#!/bin/bash

BACKUP_DIR="/backup/tls-shunt-proxy"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR

# 备份配置
tar -czf $BACKUP_DIR/config_$DATE.tar.gz config/

# 备份证书
tar -czf $BACKUP_DIR/certificates_$DATE.tar.gz certificates/

# 保留最近 7 天的备份
find $BACKUP_DIR -name "*.tar.gz" -mtime +7 -delete

echo "Backup completed: $DATE"
EOF

chmod +x /usr/local/bin/backup-tls-shunt.sh

# 添加定时任务（每天 3:00 执行）
(crontab -l 2>/dev/null; echo "0 3 * * * /usr/local/bin/backup-tls-shunt.sh") | crontab -
```

---

## 总结

### 部署清单

- [x] 创建项目目录结构
- [x] 配置 Nginx 负载均衡器
- [x] 配置 tls-shunt-proxy
- [x] 创建 Dockerfile
- [x] 创建 docker-compose.yml
- [x] 准备 SSL 证书
- [x] 构建和启动服务
- [x] 配置证书自动续期
- [x] 设置监控和日志
- [x] 配置备份策略

### 访问方式

- **HTTPS**: `https://web.xiongfei@test.com`
- **HTTP**: `http://web.xiongfei@test.com`（自动重定向到 HTTPS）
- **WebSocket**: `wss://web.xiongfei@test.com/ws`

### 管理命令

```bash
# 启动服务
cd docker && docker-compose up -d

# 停止服务
cd docker && docker-compose down

# 重启服务
cd docker && docker-compose restart

# 查看状态
cd docker && docker-compose ps

# 查看日志
cd docker && docker-compose logs -f

# 更新服务
cd docker && docker-compose pull && docker-compose up -d
```

### 端口映射

| 服务 | 容器端口 | 宿主机端口 | 说明 |
|------|----------|------------|------|
| Nginx | 80 | 80 | HTTP |
| Nginx | 443 | 443 | HTTPS |
| Nginx | 8080 | 8080 | 健康检查 |
| tls-shunt-proxy-1 | 8443 | 8443 | TLS 代理 |
| tls-shunt-proxy-1 | 8081 | 8081 | HTTP 重定向 |
| tls-shunt-proxy-2 | 8443 | 8444 | TLS 代理 |
| tls-shunt-proxy-2 | 8081 | 8082 | HTTP 重定向 |
| tls-shunt-proxy-3 | 8443 | 8445 | TLS 代理 |
| tls-shunt-proxy-3 | 8081 | 8083 | HTTP 重定向 |

### 优势

✅ **高可用性** - 多实例部署，单实例故障不影响服务
✅ **负载均衡** - Nginx 自动分发流量
✅ **易于扩展** - 可轻松添加更多实例
✅ **容器化部署** - 环境一致，易于迁移
✅ **健康检查** - 自动检测和恢复
✅ **自动重启** - 容器崩溃自动重启
✅ **日志集中** - 统一的日志管理
✅ **证书管理** - 自动续期支持

### 注意事项

⚠️ **证书路径** - 确保证书文件路径正确
⚠️ **后端可达** - 确保 Docker 网络可以访问 192.168.7.128:8080
⚠️ **防火墙** - 确保 80 和 443 端口对外开放
⚠️ **DNS 解析** - 确保 web.xiongfei@test.com 解析到服务器 IP
⚠️ **资源限制** - 根据实际负载调整容器资源限制