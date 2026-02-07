# 通配符证书支持 - 最小实现版本

## 目标

为 tls-shunt-proxy 添加通配符证书支持，通过 Cloudflare DNS 验证自动申请证书，所有子域名自动使用通配符证书。

## 核心设计

### 1. 配置结构（最小化）

在 `config/raw/raw.go` 中添加：

```go
type WildcardCertConfig struct {
    Domain          string `yaml:"domain"`            // 通配符域名，如 "*.example.com"
    DNSProvider     string `yaml:"dns_provider"`      // DNS 提供商类型：cloudflare
    DNSCredentials  string `yaml:"dns_credentials"`   // DNS 凭据（JSON 格式）
    // 保留旧配置字段以向后兼容
    CloudflareKey   string `yaml:"cloudflare_key"`
}

type RawConfig struct {
    // ... 现有字段 ...
    WildcardCerts []WildcardCertConfig `yaml:"wildcard_certs"`
}
```

### 2. 新增模块：`config/wildcard.go`

```go
package config

import (
    "context"
    "crypto/tls"
    "github.com/caddyserver/certmagic"
    "github.com/go-acme/lego/v3/challenge/dns/cloudflare"
    "github.com/liberal-boy/tls-shunt-proxy/config/raw"
    "log"
    "strings"
    "sync"
)

type WildcardManager struct {
    certs   map[string]*tls.Config  // 通配符域名 -> TLS配置
    certMap map[string]string       // 子域名 -> 通配符域名映射
    mu      sync.RWMutex
}

func NewWildcardManager() *WildcardManager {
    return &WildcardManager{
        certs:   make(map[string]*tls.Config),
        certMap: make(map[string]string),
    }
}

func (w *WildcardManager) LoadFromConfig(configs []raw.WildcardCertConfig) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    for _, wc := range configs {
        tlsConfig, err := w.getWildcardCert(wc.Domain, wc.CloudflareKey)
        if err != nil {
            log.Printf("加载通配符证书失败 %s: %v", wc.Domain, err)
            continue
        }
        w.certs[wc.Domain] = tlsConfig
        log.Printf("通配符证书已加载: %s", wc.Domain)
    }
    return nil
}

func (w *WildcardManager) getWildcardCert(domain, apiKey string) (*tls.Config, error) {
    // 设置 Cloudflare DNS 提供商（使用 libdns/cloudflare）
    cfProvider := &cloudflare.Provider{
        APIToken: apiKey,
    }

    // 使用 certmagic 管理证书
    cache := certmagic.NewCache(certmagic.CacheOptions{})
    config := certmagic.Config{
        Storage: &certmagic.FileStorage{Path: "./"},
    }

    acmeIssuer := certmagic.NewACMEIssuer(&config, certmagic.ACMEIssuer{
        DNS01Solver: &certmagic.DNS01Solver{
            DNSManager: certmagic.DNSManager{
                DNSProvider: cfProvider,
            },
        },
    })
    config.Issuers = []certmagic.Issuer{acmeIssuer}

    magic := certmagic.New(cache, config)

    // 申请通配符证书
    err := magic.ManageAsync(context.Background(), []string{domain})
    if err != nil {
        return nil, err
    }

    return &tls.Config{
        GetCertificate: magic.GetCertificate,
    }, nil
}

func (w *WildcardManager) GetCertificate(domain string) *tls.Config {
    w.mu.RLock()
    defer w.mu.RUnlock()

    // 检查缓存
    if wc, ok := w.certMap[domain]; ok {
        return w.certs[wc]
    }

    // 尝试匹配通配符
    for wildcard, tlsConfig := range w.certs {
        if w.matchWildcard(wildcard, domain) {
            w.certMap[domain] = wildcard
            return tlsConfig
        }
    }
    return nil
}

func (w *WildcardManager) matchWildcard(wildcard, domain string) bool {
    if !strings.HasPrefix(wildcard, "*.") {
        return false
    }
    suffix := wildcard[2:]
    return domain == suffix || strings.HasSuffix(domain, "."+suffix)
}
```

### 3. 修改 `config/config.go`

```go
type Config struct {
    // ... 现有字段 ...
    WildcardManager *WildcardManager
}

func ReadConfig(path string) (conf Config, err error) {
    // ... 现有代码 ...

    // 初始化通配符管理器
    conf.WildcardManager = NewWildcardManager()
    if len(rawConf.WildcardCerts) > 0 {
        if err := conf.WildcardManager.LoadFromConfig(rawConf.WildcardCerts); err != nil {
            log.Printf("加载通配符证书失败: %v", err)
        }
    }

    // ... 现有代码 ...
}
```

### 4. 修改 `config/tls.go`

```go
func getCertificateFunc(managedCert bool, serverName, cert, key, keyType, alpn, protocols string,
    wildcardManager *WildcardManager) (func(*tls.ClientHelloInfo) (*tls.Certificate, error), error) {

    return func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
        // 优先检查通配符证书
        if wildcardManager != nil {
            if wildcardCert := wildcardManager.GetCertificate(clientHello.ServerName); wildcardCert != nil {
                return wildcardCert.GetCertificate(clientHello)
            }
        }

        // 回退到原有逻辑
        // ... 现有代码 ...
    }, nil
}
```

### 5. 修改虚拟主机配置调用

在 `config/config.go` 的 `ReadConfig` 函数中：

```go
for _, vh := range rawConf.VHosts {
    var tlsConfig *tls.Config
    if vh.TlsOffloading {
        // 传递通配符管理器
        tlsConfig, err = getTlsConfig(vh.ManagedCert, vh.Name, vh.Cert, vh.Key,
            vh.KeyType, vh.Alpn, vh.Protocols, conf.WildcardManager)
    }
    // ... 其余代码 ...
}
```

## 配置示例

```yaml
listen: 0.0.0.0:443
redirecthttps: 0.0.0.0:80
inboundbuffersize: 4
outboundbuffersize: 32
fallback: 127.0.0.1:8443
webui_listen: 127.0.0.1:8080

# 通配符证书配置
wildcard_certs:
  - domain: "*.example.com"
    cloudflare_key: "your_cloudflare_api_token"

# 虚拟主机配置
vhosts:
  - name: www.example.com
    tlsoffloading: true
    managedcert: true  # 自动使用 *.example.com 证书
    keytype: p256
    alpn: h2,http/1.1
    protocols: tls12,tls13
    http:
      handler: proxyPass
      args: 127.0.0.1:8080
    default:
      handler: proxyPass
      args: 127.0.0.1:8443

  - name: api.example.com
    tlsoffloading: true
    managedcert: true  # 自动使用 *.example.com 证书
    keytype: p256
    alpn: h2,http/1.1
    protocols: tls12,tls13
    default:
      handler: proxyPass
      args: 127.0.0.1:8443

  - name: another.domain.com
    tlsoffloading: true
    managedcert: true  # 不匹配通配符，单独申请证书
    keytype: p256
    alpn: h2,http/1.1
    protocols: tls12,tls13
    default:
      handler: proxyPass
      args: 127.0.0.1:8443
```

## 实现步骤

### 步骤 1：创建通配符管理器模块
创建 `config/wildcard.go`，实现通配符证书管理逻辑。

### 步骤 2：扩展配置结构
修改 `config/raw/raw.go`，添加 `WildcardCertConfig` 和 `RawConfig.WildcardCerts`。

### 步骤 3：集成到配置加载
修改 `config/config.go`，初始化通配符管理器。

### 步骤 4：修改证书获取逻辑
修改 `config/tls.go`，在获取证书时优先检查通配符证书。

### 步骤 5：测试
- 配置通配符证书
- 启动服务
- 验证子域名自动使用通配符证书

## 依赖项

需要在 `go.mod` 中添加：

```go
require (
    github.com/libdns/cloudflare v0.1.0
)
```

## 工作流程

1. 启动时，`WildcardManager` 加载配置中的通配符证书
2. 使用 Cloudflare DNS 验证申请通配符证书（如 `*.example.com`）
3. 证书存储在 `./.caddy` 目录
4. 当请求到达时：
   - 检查请求域名是否匹配通配符
   - 如果匹配，使用通配符证书
   - 如果不匹配，回退到原有逻辑

## 特性

✅ **最小化实现**：只支持 Cloudflare DNS 验证
✅ **自动匹配**：子域名自动使用通配符证书
✅ **向后兼容**：不影响现有配置
✅ **自动续期**：利用 certmagic 自动续期
✅ **缓存优化**：子域名匹配结果缓存

## 限制

- 仅支持 Cloudflare DNS 验证
- 不支持手动 DNS 验证
- 不支持 WebUI 管理（需要手动编辑配置文件）
- 不支持自定义证书路径

## 未来扩展

- 添加手动 DNS 验证支持
- 添加 WebUI 管理界面
- 支持更多 DNS 提供商
- 添加证书状态监控

## 验证方法

1. 添加通配符证书配置
2. 启动服务，查看日志确认证书加载成功
3. 使用 `openssl s_client -connect www.example.com:443 -servername www.example.com` 验证证书
4. 检查多个子域名是否使用相同证书