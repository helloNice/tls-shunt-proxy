# tls-shunt-proxy 证书管理机制分析

## 当前实现分析

### 证书管理代码位置
- `config/tls.go` - 证书管理核心逻辑
- `config/config.go` - 配置读取和 TLS 配置初始化

### 当前实现机制

根据代码分析，当前 tls-shunt-proxy 的证书管理机制如下：

#### 1. 每个域名独立的 certmagic 实例

在 `config/tls.go` 的 `getCertificateFunc` 函数中：

```go
func getCertificateFunc(managedCert bool, serverName, cert, key, keyType string) (func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error), error) {
    // 每次调用都会创建新的 certmagic 实例
    cache := certmagic.NewCache(certmagic.CacheOptions{...})
    magic := certmagic.New(cache, config)

    if managedCert {
        err := magic.ManageAsync(context.Background(), []string{serverName})
        if err != nil {
            return nil, err
        }
    }
    // ...
}
```

**问题：**
- ❌ 每个域名调用 `getCertificateFunc` 时都会创建独立的 `certmagic.Config` 实例
- ❌ 每个域名有独立的缓存和配置对象
- ❌ 资源无法复用，大量域名时会造成内存浪费

#### 2. 存储配置

```go
config := certmagic.Config{
    Storage: &certmagic.FileStorage{Path: "./"},
    KeySource: keyGenerator,
}
```

**问题：**
- ❌ 所有域名使用相同的存储路径（`./`），但每个实例有独立的缓存
- ❌ 证书文件存储在同一目录，但每个实例独立管理

#### 3. 证书获取和管理

```go
if managedCert {
    err := magic.ManageAsync(context.Background(), []string{serverName})
    if err != nil {
        return nil, err
    }
}
```

**特点：**
- ✅ 使用 `ManageAsync` 异步管理证书
- ✅ 支持自动从 Let's Encrypt 获取证书
- ✅ 支持证书自动续期（certmagic 内置功能）

### 大量域名部署的问题

#### 问题 1：资源浪费
- 每个域名创建独立的 certmagic 实例
- 每个实例维护独立的缓存、定时器、goroutine
- 内存占用随域名数量线性增长

#### 问题 2：ACME 协议限制
- Let's Encrypt 有速率限制（如：50 个证书/小时）
- 大量域名可能触发速率限制
- 证书续期失败风险

#### 问题 3：启动性能
- 所有域名在启动时都会尝试获取/检查证书
- 大量域名会导致启动时间变长
- 如果某些域名证书获取失败，可能阻塞整个启动过程

#### 问题 4：配置分散
- 每个域名的证书配置独立管理
- 无法集中控制证书策略（如统一的 CA、续期时间等）
- 难以批量管理

### 当前代码流程

```
配置文件读取
    ↓
遍历 vhosts
    ↓
对每个 vhost 调用 getTlsConfig()
    ↓
如果 tlsoffloading=true 且 managedcert=true
    ↓
调用 getCertificateFunc()
    ↓
创建独立的 certmagic.Config 实例
    ↓
调用 magic.ManageAsync() 管理证书
    ↓
返回 magic.GetCertificate 作为 TLS 握手回调
```

## 改进方向建议

### 1. 共享 certmagic 实例
创建一个全局共享的 certmagic 实例，所有使用 managedcert 的域名都通过这个实例管理

### 2. 批量证书管理
使用 certmagic 的批量管理功能，一次性管理多个域名

### 3. 懒加载证书
启动时不立即获取所有证书，而是在首次连接时按需获取

### 4. 证书池管理
实现证书池机制，定期检查和续期即将过期的证书

### 5. 监控和告警
添加证书过期监控和告警机制