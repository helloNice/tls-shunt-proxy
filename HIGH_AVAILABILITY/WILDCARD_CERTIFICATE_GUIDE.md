# tls-shunt-proxy 通配符证书配置指南

## 概述

通配符证书（Wildcard Certificate）可以保护一个主域名及其所有子域名，例如：

- 主域名：`xiongfei@test.com`
- 通配符证书：`*.xiongfei@test.com`
- 保护范围：
  - `web.xiongfei@test.com` ✅
  - `api.xiongfei@test.com` ✅
  - `admin.xiongfei@test.com` ✅
  - `xiongfei@test.com` ❌（需要单独申请或使用 SAN）

## 优势

✅ **成本更低** - 一个证书保护所有子域名
✅ **管理简单** - 只需管理一个证书
✅ **自动续期** - 一次续期，全部生效
✅ **减少配置** - 无需为每个子域名配置证书路径

## 注意事项

⚠️ **DNS 验证要求** - 通配符证书必须使用 DNS 验证，不能使用 HTTP 验证
⚠️ **不包括主域名** - `*.xiongfei@test.com` 不包括 `xiongfei@test.com`
⚠️ **仅限一级子域名** - 不保护二级子域名（如 `*.api.xiongfei@test.com`）

---

## 方案一：使用 certbot 申请通配符证书

### 前提条件

1. 域名已解析到服务器
2. 拥有域名 DNS 管理权限
3. 安装 certbot

### 安装 certbot

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install certbot -y

# CentOS/RHEL
sudo yum install certbot -y

# macOS
brew install certbot
```

### 申请通配符证书

#### 方法 1：使用 Cloudflare DNS 插件（推荐）

```bash
# 安装 Cloudflare DNS 插件
sudo apt-get install python3-certbot-dns-cloudflare -y

# 创建 Cloudflare API 凭据文件
mkdir -p ~/.secrets
chmod 700 ~/.secrets

cat > ~/.secrets/cloudflare.ini << 'EOF'
dns_cloudflare_api_token = YOUR_CLOUDFLARE_API_TOKEN
EOF

chmod 600 ~/.secrets/cloudflare.ini

# 申请通配符证书
sudo certbot certonly \
  --dns-cloudflare \
  --dns-cloudflare-credentials ~/.secrets/cloudflare.ini \
  --dns-cloudflare-propagation-seconds 30 \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com" \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive
```

#### 方法 2：使用其他 DNS 插件

```bash
# 支持的 DNS 插件：
# - certbot-dns-cloudflare
# - certbot-dns-aliyun
# - certbot-dns-tencentcloud
# - certbot-dns-dnspod
# - certbot-dns-digitalocean
# - certbot-dns-google
# - certbot-dns-route53

# 以阿里云为例
sudo apt-get install python3-certbot-dns-aliyun -y

cat > ~/.secrets/aliyun.ini << 'EOF'
dns_aliyun_access_key = YOUR_ACCESS_KEY
dns_aliyun_access_key_secret = YOUR_ACCESS_SECRET
EOF

chmod 600 ~/.secrets/aliyun.ini

sudo certbot certonly \
  --dns-aliyun \
  --dns-aliyun-credentials ~/.secrets/aliyun.ini \
  --dns-aliyun-propagation-seconds 60 \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com" \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive
```

#### 方法 3：手动 DNS 验证

```bash
# 申请证书，手动添加 DNS TXT 记录
sudo certbot certonly \
  --manual \
  --preferred-challenges dns \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com" \
  --email your-email@example.com \
  --agree-tos

# 按照提示，在 DNS 管理面板添加 TXT 记录
# _acme-challenge.xiongfei@test.com  TXT  <提供的验证值>
```

### 证书位置

证书获取成功后，保存在：
```
/etc/letsencrypt/live/xiongfei@test.com/
├── fullchain.pem    # 完整证书链
├── privkey.pem      # 私钥
├── cert.pem         # 证书
└── chain.pem        # 中间证书
```

### 续期证书

```bash
# 测试续期
sudo certbot renew --dry-run

# 自动续期
sudo certbot renew

# 添加定时任务（每天 2:00 检查）
(crontab -l 2>/dev/null; echo "0 2 * * * certbot renew --quiet && systemctl reload nginx") | crontab -
```

---

## 方案二：使用 acme.sh 申请通配符证书

### 安装 acme.sh

```bash
curl https://get.acme.sh | sh
source ~/.bashrc
```

### 申请通配符证书

#### 使用 Cloudflare DNS

```bash
# 设置 Cloudflare API Token
export CF_Token="YOUR_CLOUDFLARE_API_TOKEN"

# 申请通配符证书
acme.sh --issue --dns dns_cf \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com"
```

#### 使用阿里云 DNS

```bash
# 设置阿里云 API
export Ali_Key="YOUR_ACCESS_KEY"
export Ali_Secret="YOUR_ACCESS_SECRET"

# 申请通配符证书
acme.sh --issue --dns dns_ali \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com"
```

#### 腾讯云 DNS

```bash
# 设置腾讯云 API
export DP_Id="YOUR_ID"
export DP_Key="YOUR_KEY"

# 申请通配符证书
acme.sh --issue --dns dns_dp \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com"
```

### 安装证书

```bash
# 安装证书到指定目录
acme.sh --install-cert -d "*.xiongfei@test.com" \
  --cert-file /path/to/cert.pem \
  --key-file /path/to/key.pem \
  --fullchain-file /path/to/fullchain.pem \
  --reloadcmd "systemctl reload nginx"
```

### 续期证书

```bash
# acme.sh 会自动续期
# 默认每天 0:00 检查续期

# 手动续期
acme.sh --renew -d "*.xiongfei@test.com" --force
```

---

## 方案三：使用 tls-shunt-proxy 的 managedcert 自动获取

### 配置说明

当前 tls-shunt-proxy 的 `managedcert` 功能使用 certmagic，支持通配符证书，但需要配置 DNS 提供商。

### 修改 config/tls.go

需要添加 DNS 提供商配置。以下是修改后的代码示例：

```go
// 在 config/tls.go 中添加 DNS 提供商配置
func getTlsConfig(managedCert bool, serverName, cert, key, keyType, alpn, protocols string) (*tls.Config, error) {
    certificateFunc, err := getCertificateFunc(managedCert, serverName, cert, key, keyType)
    if err != nil {
        return nil, err
    }

    // ... 其余代码保持不变
}

func getCertificateFunc(managedCert bool, serverName, cert, key, keyType string) (func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error), error) {
    var keyGenerator = certmagic.DefaultKeyGenerator
    if keyType != "" {
        keyGenerator = certmagic.StandardKeyGenerator{KeyType: certmagic.KeyType(keyType)}
    }

    // 配置 DNS 提供商（示例：Cloudflare）
    dnsProvider := certmagic.DNSProvider{
        Name: "cloudflare",
        Config: map[string]string{
            "CLOUDFLARE_API_TOKEN": os.Getenv("CLOUDFLARE_API_TOKEN"),
        },
    }

    config := certmagic.Config{
        Storage:   &certmagic.FileStorage{Path: "./"},
        KeySource: keyGenerator,
        DNS:       &dnsProvider,  // 添加 DNS 提供商
    }

    var magic *certmagic.Config

    cache := certmagic.NewCache(certmagic.CacheOptions{
        GetConfigForCert: func(certificate certmagic.Certificate) (c *certmagic.Config, err error) {
            return magic, nil
        },
    })

    magic = certmagic.New(cache, config)

    if managedCert {
        // 支持通配符域名
        domains := []string{serverName}
        if strings.HasPrefix(serverName, "*.") {
            // 通配符域名，同时包含主域名
            mainDomain := strings.TrimPrefix(serverName, "*.")
            domains = append(domains, mainDomain)
        }

        err := magic.ManageAsync(context.Background(), domains)
        if err != nil {
            return nil, err
        }
    } else {
        _, err := magic.CacheUnmanagedCertificatePEMFile(context.TODO(), cert, key, nil)
        if err != nil {
            err = fmt.Errorf("fail to load tls key pair for %s: %v", serverName, err)
            return nil, err
        }
    }

    return magic.GetCertificate, nil
}
```

### 配置文件示例

```yaml
# config.yaml
vhosts:
  # 通配符证书配置
  - name: "*.xiongfei@test.com"

    tlsoffloading: true
    managedcert: true
    keytype: p256
    alpn: h2,http/1.1
    protocols: tls12,tls13

    http:
      paths:
        - path: "/"
          handler: proxyPass
          args: 192.168.7.128:8080

    default:
      handler: proxyPass
      args: 192.168.7.128:8080

  # 主域名配置（如果需要）
  - name: "xiongfei@test.com"

    tlsoffloading: true
    managedcert: true
    keytype: p256
    alpn: h2,http/1.1
    protocols: tls12,tls13

    http:
      paths:
        - path: "/"
          handler: proxyPass
          args: 192.168.7.128:8080

    default:
      handler: proxyPass
      args: 192.168.7.128:8080
```

### 环境变量配置

```bash
# 设置 Cloudflare API Token
export CLOUDFLARE_API_TOKEN="your_api_token_here"

# 启动 tls-shunt-proxy
./tls-shunt-proxy -config config.yaml
```

---

## tls-shunt-proxy 配置示例

### 方案 A：使用本地通配符证书

```yaml
# config.yaml
vhosts:
  # 所有子域名使用同一个通配符证书
  - name: "web.xiongfei@test.com"
    tlsoffloading: true
    managedcert: false
    cert: /certificates/xiongfei@test.com/fullchain.pem
    key: /certificates/xiongfei@test.com/privkey.pem
    alpn: h2,http/1.1
    protocols: tls12,tls13

    http:
      paths:
        - path: "/"
          handler: proxyPass
          args: 192.168.7.128:8080

    default:
      handler: proxyPass
      args: 192.168.7.128:8080

  - name: "api.xiongfei@test.com"
    tlsoffloading: true
    managedcert: false
    # 使用相同的通配符证书
    cert: /certificates/xiongfei@test.com/fullchain.pem
    key: /certificates/xiongfei@test.com/privkey.pem
    alpn: h2,http/1.1
    protocols: tls12,tls13

    http:
      paths:
        - path: "/"
          handler: proxyPass
          args: 192.168.7.128:8081

    default:
      handler: proxyPass
      args: 192.168.7.128:8081

  - name: "admin.xiongfei@test.com"
    tlsoffloading: true
    managedcert: false
    # 使用相同的通配符证书
    cert: /certificates/xiongfei@test.com/fullchain.pem
    key: /certificates/xiongfei@test.com/privkey.pem
    alpn: h2,http/1.1
    protocols: tls12,tls13

    http:
      paths:
        - path: "/"
          handler: proxyPass
          args: 192.168.7.128:8082

    default:
      handler: proxyPass
      args: 192.168.7.128:8082
```

### 方案 B：使用通配符域名配置（简化版）

```yaml
# config.yaml
vhosts:
  # 使用通配符域名配置
  - name: "*.xiongfei@test.com"
    tlsoffloading: true
    managedcert: false
    cert: /certificates/xiongfei@test.com/fullchain.pem
    key: /certificates/xiongfei@test.com/privkey.pem
    alpn: h2,http/1.1
    protocols: tls12,tls13

    # 根据子域名路由到不同的后端
    http:
      paths:
        - path: "/"
          handler: proxyPass
          args: 192.168.7.128:8080

    default:
      handler: proxyPass
      args: 192.168.7.128:8080
```

### 方案 C：Docker 部署使用通配符证书

```yaml
# docker/docker-compose.yml
services:
  nginx:
    image: nginx:alpine
    container_name: nginx-lb
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - ../certificates:/etc/nginx/ssl:ro
    depends_on:
      - tls-shunt-proxy-1
      - tls-shunt-proxy-2
      - tls-shunt-proxy-3
    networks:
      - tls-shunt-network
    restart: unless-stopped

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
    networks:
      - tls-shunt-network
    restart: unless-stopped

networks:
  tls-shunt-network:
    driver: bridge
```

```nginx
# docker/nginx/nginx.conf
upstream tls_shunt_proxy_backend {
    server tls-shunt-proxy-1:8443;
    server tls-shunt-proxy-2:8443;
    server tls-shunt-proxy-3:8443;
}

server {
    listen 443 ssl http2;
    server_name web.xiongfei@test.com api.xiongfei@test.com admin.xiongfei@test.com;

    # 使用通配符证书
    ssl_certificate /etc/nginx/ssl/xiongfei@test.com/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/xiongfei@test.com/privkey.pem;

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    location / {
        proxy_pass https://tls_shunt_proxy_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

---

## 证书目录结构

```
certificates/
└── xiongfei@test.com/
    ├── fullchain.pem      # 通配符证书链
    ├── privkey.pem        # 私钥
    ├── cert.pem           # 证书文件
    └── chain.pem          # 中间证书链
```

所有子域名（`web.xiongfei@test.com`、`api.xiongfei@test.com`、`admin.xiongfei@test.com`）都使用这个通配符证书。

---

## 验证证书

```bash
# 检查通配符证书
openssl x509 -in /etc/letsencrypt/live/xiongfei@test.com/fullchain.pem -noout -text | grep DNS

# 输出应该包含：
# DNS:*.xiongfei@test.com, DNS:xiongfei@test.com

# 测试 SSL 连接
openssl s_client -connect web.xiongfei@test.com:443 -servername web.xiongfei@test.com
openssl s_client -connect api.xiongfei@test.com:443 -servername api.xiongfei@test.com
```

---

## 自动续期脚本

```bash
#!/bin/bash
# /usr/local/bin/renew-wildcard-cert.sh

# 续期证书
certbot renew --quiet --dns-cloudflare --dns-cloudflare-credentials ~/.secrets/cloudflare.ini

# 复制到项目目录
cp /etc/letsencrypt/live/xiongfei@test.com/fullchain.pem /path/to/certificates/xiongfei@test.com/
cp /etc/letsencrypt/live/xiongfei@test.com/privkey.pem /path/to/certificates/xiongfei@test.com/

# 重新加载 Nginx
docker exec nginx-lb nginx -s reload

# 重启 tls-shunt-proxy 容器
docker restart tls-shunt-proxy-1 tls-shunt-proxy-2 tls-shunt-proxy-3

echo "Certificate renewed and services reloaded at $(date)"
```

```bash
# 添加定时任务
chmod +x /usr/local/bin/renew-wildcard-cert.sh
(crontab -l 2>/dev/null; echo "0 2 * * * /usr/local/bin/renew-wildcard-cert.sh >> /var/log/cert-renew.log 2>&1") | crontab -
```

---

## 常见问题

### Q1: 通配符证书是否包括主域名？

**A:** 默认不包括。需要同时申请 `*.xiongfei@test.com` 和 `xiongfei@test.com`。

```bash
certbot certonly --dns-cloudflare \
  -d "*.xiongfei@test.com" \
  -d "xiongfei@test.com"
```

### Q2: 如何保护二级子域名？

**A:** 需要申请多个通配符证书。

```bash
# 一级子域名
certbot certonly --dns-cloudflare -d "*.xiongfei@test.com"

# 二级子域名
certbot certonly --dns-cloudflare -d "*.api.xiongfei@test.com"
```

### Q3: 通配符证书是否支持多域名？

**A:** 支持，可以在一个证书中包含多个通配符域名。

```bash
certbot certonly --dns-cloudflare \
  -d "*.xiongfei@test.com" \
  -d "*.example.com"
```

### Q4: Let's Encrypt 通配符证书的有效期？

**A:** 90 天，需要定期续期。

### Q5: 是否可以限制通配符证书的用途？

**A:** 可以，通过证书的扩展字段限制用途，但 Let's Encrypt 通配符证书默认支持所有用途。

---

## 总结

### 推荐方案

| 场景 | 推荐方案 | 说明 |
|------|----------|------|
| **测试环境** | 自签名通配符证书 | 免费，但浏览器不信任 |
| **生产环境（Cloudflare）** | certbot + Cloudflare DNS | 自动化程度高 |
| **生产环境（阿里云）** | certbot + 阿里云 DNS | 国内访问快 |
| **生产环境（腾讯云）** | certbot + 腾讯云 DNS | 国内访问快 |
| **多域名管理** | acme.sh | 支持更多 DNS 提供商 |

### 最佳实践

1. ✅ 使用 DNS 验证申请通配符证书
2. ✅ 同时申请 `*.domain.com` 和 `domain.com`
3. ✅ 配置自动续期
4. ✅ 所有子域名使用同一个通配符证书
5. ✅ 定期检查证书有效期
6. ✅ 备份证书和私钥

### 配置检查清单

- [ ] 申请通配符证书（包含主域名）
- [ ] 配置 DNS 验证
- [ ] 设置自动续期
- [ ] 配置 tls-shunt-proxy 使用通配符证书
- [ ] 测试所有子域名访问
- [ ] 验证证书有效期
- [ ] 配置监控和告警

### 优势对比

| 项目 | 单域名证书 | 通配符证书 |
|------|------------|------------|
| 成本 | 每个域名单独付费 | 一个证书保护所有子域名 |
| 管理 | 需要管理多个证书 | 只需管理一个证书 |
| 续期 | 每个证书单独续期 | 一次续期，全部生效 |
| 配置 | 每个域名配置证书路径 | 所有域名使用相同证书 |
| 安全性 | 影响范围小 | 影响范围大（所有子域名） |

### 注意事项

⚠️ 通配符证书影响所有子域名，续期失败会影响所有服务
⚠️ 需要确保 DNS 提供商 API 安全
⚠️ Let's Encrypt 有速率限制，避免频繁申请
⚠️ 建议使用测试环境先验证配置