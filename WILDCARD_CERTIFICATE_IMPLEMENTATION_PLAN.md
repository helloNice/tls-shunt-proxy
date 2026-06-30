# 通配符证书支持功能实现计划

## 需求概述

增加一个配置模块，支持为同一个域名下的多个子域名申请和使用通配符证书，而不是为每个子域名单独申请证书。

### 功能要求
1. 配置一个通配符域名列表（如 `*.example.com`）
2. 支持手动配置 DNS 验证
3. 自动申请通配符证书
4. 对于配置文件中的子域名，即使启用了 `managedCert`，也优先使用通配符证书
5. 创建独立模块处理此功能，确保不影响现有逻辑

## 设计方案

### 1. 配置结构扩展

#### 1.1 新增配置项
在 `config/raw/raw.go` 中添加通配符证书配置结构：

```go
type WildcardCertConfig struct {
    Domain         string `yaml:"domain"`           // 通配符域名，如 "*.example.com"
    DNSProvider    string `yaml:"dns_provider"`     // DNS 提供商类型（manual, cloudflare等）
    DNSCredentials string `yaml:"dns_credentials"`  // DNS 凭据（JSON格式或API密钥）
    Managed        bool   `yaml:"managed"`           // 是否自动管理（申请/续期）
    CertPath       string `yaml:"cert_path"`        // 自定义证书路径（可选）
    KeyPath        string `yaml:"key_path"`         // 自定义密钥路径（可选）
}

type RawConfig struct {
    // ... 现有字段 ...
    WildcardCerts []WildcardCertConfig `yaml:"wildcard_certs"` // 新增：通配符证书配置列表
}
```

### 2. 新增模块：`config/wildcard_cert.go`

#### 2.1 模块职责
- 管理通配符证书的申请、加载和匹配
- 提供通配符证书查询接口
- 支持 DNS 验证（手动或自动）
- 处理通配符域名与子域名的匹配逻辑

#### 2.2 核心接口

```go
// WildcardCertManager 通配符证书管理器
type WildcardCertManager struct {
    certs    map[string]*tls.Config  // 通配符域名 -> TLS配置
    certMap  map[string]string       // 子域名 -> 通配符域名映射
    mu       sync.RWMutex
}

// NewWildcardCertManager 创建通配符证书管理器
func NewWildcardCertManager() *WildcardCertManager

// LoadFromConfig 从配置加载通配符证书
func (w *WildcardCertManager) LoadFromConfig(wildcardConfigs []raw.WildcardCertConfig) error

// GetCertificate 获取指定域名的证书（优先返回通配符证书）
func (w *WildcardCertManager) GetCertificate(domain string) *tls.Config

// GetWildcardDomain 获取匹配的通配符域名
func (w *WildcardCertManager) GetWildcardDomain(domain string) string

// IsWildcardManaged 检查域名是否由通配符证书管理
func (w *WildcardCertManager) IsWildcardManaged(domain string) bool
```

#### 2.3 DNS 验证接口

```go
// DNSChallengeProvider DNS挑战提供者接口
type DNSChallengeProvider interface {
    Present(domain, token, keyAuth string) error
    CleanUp(domain, token, keyAuth string) error
}

// ManualDNSProvider 手动DNS验证
type ManualDNSProvider struct{}

// CloudflareDNSProvider Cloudflare DNS验证
type CloudflareDNSProvider struct {
    APIKey    string
    AccountID string
}
```

### 3. 修改现有模块

#### 3.1 修改 `config/tls.go`
在 `getCertificateFunc` 中添加通配符证书检查逻辑：

```go
func getCertificateFunc(managedCert bool, serverName, cert, key, keyType string,
    wildcardManager *WildcardCertManager) (func(*tls.ClientHelloInfo) (*tls.Certificate, error), error) {

    return func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
        // 优先检查通配符证书
        if wildcardManager != nil {
            if wildcardCert := wildcardManager.GetCertificate(clientHello.ServerName); wildcardCert != nil {
                // 使用通配符证书
                return wildcardManager.GetCertificate(clientHello.ServerName), nil
            }
        }

        // 回退到原有逻辑
        // ... 现有代码 ...
    }, nil
}
```

#### 3.2 修改 `config/config.go`
在 `ReadConfig` 中初始化通配符证书管理器：

```go
type Config struct {
    // ... 现有字段 ...
    WildcardCertManager *WildcardCertManager // 新增：通配符证书管理器
}

func ReadConfig(path string) (conf Config, err error) {
    // ... 现有代码 ...

    // 初始化通配符证书管理器
    conf.WildcardCertManager = NewWildcardCertManager()
    if len(rawConf.WildcardCerts) > 0 {
        if err := conf.WildcardCertManager.LoadFromConfig(rawConf.WildcardCerts); err != nil {
            log.Printf("加载通配符证书失败: %v", err)
        }
    }

    // ... 现有代码 ...
}
```

#### 3.3 修改 `config/config.go` 中的虚拟主机配置
在处理每个虚拟主机时，检查是否被通配符证书管理：

```go
for _, vh := range rawConf.VHosts {
    // 检查是否被通配符证书管理
    isWildcardManaged := conf.WildcardCertManager != nil &&
                         conf.WildcardCertManager.IsWildcardManaged(vh.Name)

    var tlsConfig *tls.Config
    if vh.TlsOffloading {
        // 如果被通配符管理，则禁用自动证书申请
        effectiveManagedCert := vh.ManagedCert && !isWildcardManaged
        tlsConfig, err = getTlsConfig(effectiveManagedCert, vh.Name, vh.Cert, vh.Key,
            vh.KeyType, vh.Alpn, vh.Protocols, conf.WildcardCertManager)
    }
    // ... 其余代码 ...
}
```

### 4. WebUI 扩展

#### 4.1 新增 API 端点（`webui/server.go`）

```go
// API: 获取通配符证书列表
mux.HandleFunc("/api/wildcard-certs", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "仅支持 GET", http.StatusMethodNotAllowed)
        return
    }
    // 返回通配符证书列表和状态
})

// API: 添加通配符证书配置
mux.HandleFunc("/api/wildcard-certs", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
        return
    }
    // 添加新的通配符证书配置
})

// API: 删除通配符证书配置
mux.HandleFunc("/api/wildcard-certs/{domain}", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, "仅支持 DELETE", http.StatusMethodNotAllowed)
        return
    }
    // 删除指定的通配符证书配置
})

// API: 手动触发通配符证书申请
mux.HandleFunc("/api/wildcard-certs/{domain}/issue", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
        return
    }
    // 手动触发证书申请，返回DNS验证所需的记录
})

// API: 确认DNS验证完成并继续证书申请
mux.HandleFunc("/api/wildcard-certs/{domain}/verify", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
        return
    }
    // 确认DNS记录已添加，继续完成证书申请
})
```

#### 4.2 前端页面扩展（`webui/templates/index.html`）
添加通配符证书管理界面：
- 显示当前通配符证书列表
- 添加通配符证书表单（域名、DNS提供商、凭据）
- 显示DNS验证指导
- 证书状态显示（申请中、已颁发、即将过期等）

### 5. 实现步骤

#### 阶段一：核心模块开发（不影响现有功能）
1. ✅ 创建 `config/wildcard_cert.go` 模块
2. ✅ 实现通配符域名匹配逻辑
3. ✅ 实现 DNS 验证接口（至少支持手动验证）
4. ✅ 实现通配符证书管理器
5. ✅ 单元测试

#### 阶段二：配置结构扩展
1. ✅ 扩展 `config/raw/raw.go` 配置结构
2. ✅ 确保向后兼容（可选字段，不影响现有配置）
3. ✅ 配置文件示例更新

#### 阶段三：集成现有逻辑
1. ✅ 修改 `config/tls.go` 添加通配符证书优先逻辑
2. ✅ 修改 `config/config.go` 集成通配符管理器
3. ✅ 测试确保现有虚拟主机配置不受影响
4. ✅ 回归测试

#### 阶段四：WebUI 支持
1. ✅ 添加通配符证书管理 API
2. ✅ 扩展前端界面
3. ✅ DNS 验证流程支持
4. ✅ 证书状态监控

#### 阶段五：文档和测试
1. ✅ 编写使用文档
2. ✅ 提供配置示例
3. ✅ 端到端测试
4. ✅ 性能测试

### 6. 配置示例

```yaml
# config.yaml 示例

listen: 0.0.0.0:443
redirecthttps: 0.0.0.0:80
inboundbuffersize: 4
outboundbuffersize: 32
fallback: 127.0.0.1:8443
webui_listen: 127.0.0.1:8080

# 通配符证书配置
wildcard_certs:
  - domain: "*.example.com"
    dns_provider: manual
    managed: true
    # cert_path 和 key_path 可选，如果不指定则使用默认路径
    # cert_path: "./certs/wildcard.example.com.crt"
    # key_path: "./certs/wildcard.example.com.key"

  - domain: "*.app.example.com"
    dns_provider: cloudflare
    dns_credentials: '{"api_key": "your_api_key", "account_id": "your_account_id"}'
    managed: true

# 虚拟主机配置
vhosts:
  - name: www.example.com
    tlsoffloading: true
    managedcert: true  # 即使为 true，也会使用 *.example.com 的通配符证书
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
    managedcert: true  # 自动使用 *.example.com 的通配符证书
    # ... 其他配置 ...

  - name: another.domain.com
    tlsoffloading: true
    managedcert: true  # 这个域名不匹配任何通配符，将单独申请证书
    # ... 其他配置 ...
```

### 7. DNS 验证流程（手动模式）

1. 用户在 WebUI 中添加通配符证书配置（如 `*.example.com`）
2. 点击"申请证书"按钮
3. 系统返回需要添加的 DNS TXT 记录：
   ```
   _acme-challenge.example.com  TXT  "xxxxx验证码xxxxx"
   ```
4. 用户在 DNS 管理界面添加此记录
5. 用户在 WebUI 中点击"验证并继续"
6. 系统验证 DNS 记录并完成证书申请
7. 证书保存到指定位置，自动续期

### 8. 通配符匹配逻辑

```go
// 匹配规则：
// 1. 精确匹配：example.com 匹配 example.com
// 2. 通配符匹配：*.example.com 匹配 www.example.com, api.example.com
// 3. 通配符不匹配根域名：*.example.com 不匹配 example.com
// 4. 多级通配符：*.app.example.com 匹配 api.app.example.com
// 5. 优先级：精确匹配 > 最长通配符 > 最短通配符
```

### 9. 证书存储

默认路径结构：
```
./.caddy/
  └── acme/
      └── certificates/
          └── wildcard.example.com/
              ├── wildcard.example.com.crt
              └── wildcard.example.com.key
```

### 10. 向后兼容性

- 所有新配置项均为可选
- 如果未配置 `wildcard_certs`，行为与原版本完全一致
- 现有配置文件无需修改即可继续使用
- 通配符证书管理仅在配置启用时生效

### 11. 错误处理

- 通配符证书申请失败不影响其他虚拟主机
- 通配符证书加载失败会记录日志并回退到原有逻辑
- DNS 验证失败会提供详细错误信息

### 12. 性能考虑

- 通配符证书查询使用缓存和读写锁
- 通配符匹配算法优化为 O(n)，n 为通配符配置数量
- 证书续期在后台异步进行，不影响服务

### 13. 安全考虑

- DNS 凭据加密存储（未来可考虑）
- API 密钥不暴露在日志中
- 证书文件权限设置为 600
- 支持证书自动续期前通知（未来可考虑）

### 14. 测试计划

#### 单元测试
- 通配符匹配逻辑测试
- DNS 验证接口测试
- 证书管理器测试

#### 集成测试
- 配置加载测试
- 通配符证书优先级测试
- 多通配符配置测试

#### 端到端测试
- 完整的 DNS 验证流程
- 证书申请和自动续期
- 与现有虚拟主机配置的兼容性

### 15. 风险评估

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| 通配符证书申请失败 | 中 | 回退到原有逻辑，不影响其他虚拟主机 |
| DNS 验证超时 | 低 | 提供重试机制和详细错误信息 |
| 证书续期失败 | 高 | 实现续期失败告警（未来） |
| 性能下降 | 低 | 使用缓存和优化匹配算法 |
| 破坏现有功能 | 高 | 充分测试，所有新功能可选 |

### 16. 时间估算

- 阶段一：核心模块开发 - 2-3 天
- 阶段二：配置结构扩展 - 1 天
- 阶段三：集成现有逻辑 - 2 天
- 阶段四：WebUI 支持 - 3-4 天
- 阶段五：文档和测试 - 2 天

**总计：约 10-12 天**

### 17. 依赖项

现有依赖已满足：
- `github.com/caddyserver/certmagic` - 证书管理
- `github.com/go-acme/lego/v3` - ACME 协议实现

可能需要新增：
- `github.com/go-acme/lego/v3/challenge/dns/cloudflare` - Cloudflare DNS 验证

### 18. 未来扩展

- 支持更多 DNS 提供商（阿里云、腾讯云等）
- 支持证书吊销
- 证书到期前告警
- 支持多 SAN 证书
- 证书自动轮换策略

## 总结

本计划通过创建独立的通配符证书管理模块，在不影响现有功能的前提下，实现了通配符证书的支持。核心设计理念是：

1. **独立性**：新功能完全独立，不修改现有核心逻辑
2. **可选性**：所有新配置均为可选，向后兼容
3. **优先级**：通配符证书优先于单独证书
4. **易用性**：提供 WebUI 界面简化操作
5. **可靠性**：完善的错误处理和回退机制

通过分阶段实施，可以确保每个阶段都能独立测试和验证，降低整体风险。