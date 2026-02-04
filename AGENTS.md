# TLS 分流器 - 项目分析文档

## 项目概述

这是一个用于分流 TLS 流量的 Go 语言代理服务器，专为 vmess + TLS + Web 方案设计，支持与 trojan 共享端口。该项目是一个基于 SNI（Server Name Indication）的智能 TLS 代理，可以根据不同的 SNI 和协议类型将流量分发到不同的后端服务。

**版本**: 0.9.3
**主要语言**: Go (Go 1.22.0+)
**许可证**: 未明确说明（从仓库结构判断可能是开源项目）

## 核心功能

1. **SNI 分流**: 根据 TLS SNI 扩展的 server name 将流量分发到不同的虚拟主机
2. **流量类型识别**: 自动识别 HTTP、HTTP/2、Trojan 等不同协议类型
3. **TLS 卸载**: 支持解开 TLS 加密以识别和分发流量
4. **自动证书管理**: 支持从 LetsEncrypt 自动获取和续期 SSL/TLS 证书
5. **HTTP 路由**: 基于 HTTP path 的流量路由功能
6. **静态文件服务**: 内置静态网站服务器，支持 h2c
7. **HTTPS 重定向**: 自动将 HTTP 流量重定向到 HTTPS
8. **Proxy Protocol**: 支持将源 IP 信息通过 Proxy Protocol 传递给后端
9. **Web 管理界面**: 内置基于 Web 的配置管理界面（带 Basic Auth）

## 技术架构

### 目录结构

```
tls-shunt-proxy/
├── main.go              # 主程序入口
├── config/              # 配置处理
│   ├── config.go        # 配置读取和解析
│   ├── tls.go           # TLS 证书管理
│   └── raw/             # 原始配置结构定义
├── handler/             # 流量处理器
│   ├── conn_listener.go
│   ├── doh_server.go    # DNS-over-HTTPS 服务器
│   ├── file_server.go   # 静态文件服务器
│   ├── handler.go       # Handler 接口定义
│   ├── noop.go          # 空操作处理器
│   ├── plain_text.go    # 纯文本处理器
│   ├── proxy_pass.go    # 代理转发处理器
│   ├── redirect_https.go # HTTPS 重定向处理器
│   ├── dns/             # DNS 相关处理器
│   └── http2/           # HTTP/2 处理器
├── sniffer/             # 流量嗅探器
│   └── sniff_conn.go    # 连接流量嗅探
└── webui/               # Web 管理界面
    └── server.go        # Web UI 服务器
```

### 核心组件

#### 1. 主程序 (main.go)
- 监听 TCP 连接
- 提取 SNI 信息
- 根据流量类型调用对应的处理器
- 版本: 0.9.3

#### 2. 配置管理 (config/)
- **config.go**: YAML 配置文件解析、虚拟主机配置管理
- **tls.go**: TLS 证书管理，支持自动从 LetsEncrypt 获取证书
- 使用 certmagic 库进行证书管理

#### 3. 流量处理器 (handler/)
实现了多种 Handler 接口：
- **proxy_pass**: TCP 代理转发，支持 Unix domain socket 和 Proxy Protocol
- **file_server**: 静态文件服务器，支持 h2c
- **doh_server**: DNS-over-HTTPS 服务器
- **redirect_https**: HTTP 到 HTTPS 重定向
- **noop**: 空操作（用于未配置的情况）

#### 4. 流量嗅探器 (sniffer/)
- **SniffConn**: 包装 net.Conn，在读取前预览数据
- 支持识别的协议类型：
  - HTTP (GET, POST, HEAD, PUT, DELETE, OPTIONS, CONNECT)
  - HTTP/2 (PRI * HTTP/2.0)
  - Trojan (特定字节特征)
  - 未知流量

#### 5. Web 管理界面 (webui/)
- 绑定到 127.0.0.1:8080
- 支持 Basic Auth（默认 admin/admin，可通过环境变量 WEBUI_USER/WEBUI_PASS 配置）
- 提供配置文件在线编辑和保存功能
- 保存配置后自动重启服务

## 构建和运行

### 构建项目

```bash
# 编译项目
go build -o tls-shunt-proxy main.go

# 交叉编译其他平台（示例）
GOOS=linux GOARCH=amd64 go build -o tls-shunt-proxy-linux-amd64 main.go
```

### 运行项目

```bash
# 使用默认配置文件路径
./tls-shunt-proxy

# 指定配置文件路径
./tls-shunt-proxy -config /path/to/config.yaml
```

### Linux 安装脚本

对于 linux-amd64 平台，可以使用提供的安装脚本：

```bash
bash <(curl -L -s https://raw.githubusercontent.com/liberal-boy/tls-shunt-proxy/master/dist/install.sh)
```

安装后：
- 可执行文件位于: `/usr/local/bin/tls-shunt-proxy`
- 配置文件位于: `/etc/tls-shunt-proxy/config.yaml`

### 服务权限

如果服务启动失败（绑定特权端口），需要赋予 CAP_NET_BIND_SERVICE 权限：

```bash
sudo setcap "cap_net_bind_service=+ep" /usr/local/bin/tls-shunt-proxy
```

### Web 管理界面

Web UI 自动在 127.0.0.1:8080 启动，访问：
```
http://127.0.0.1:8080
```

默认凭据: admin/admin
可通过环境变量自定义：
```bash
export WEBUI_USER=your_username
export WEBUI_PASS=your_password
```

## 配置文件

配置文件采用 YAML 格式，主要配置项包括：

### 基础配置
- `listen`: 监听地址（默认 0.0.0.0:443）
- `redirecthttps`: HTTP 重定向监听地址（默认 0.0.0.0:80）
- `inboundbuffersize`: 入站缓冲区大小（KB，默认 4）
- `outboundbuffersize`: 出站缓冲区大小（KB，默认 32）
- `fallback`: 未识别 SNI 的回落地址

### 虚拟主机配置 (vhosts)
每个虚拟主机包含：
- `name`: SNI server name
- `tlsoffloading`: 是否解开 TLS（true/false）
- `managedcert`: 是否自动管理证书（LetsEncrypt）
- `cert`/`key`: 自定义证书和密钥路径
- `keytype`: 证书密钥类型（ed25519、p256、p384、rsa2048、rsa4096、rsa8192）
- `alpn`: ALPN 协议列表
- `protocols`: TLS 版本范围（tls12,tls13）
- `http`: HTTP 流量处理配置
- `http2`: HTTP/2 流量处理配置
- `trojan`: Trojan 流量处理配置
- `default`: 默认流量处理配置

### Handler 类型
- `proxyPass`: 转发到指定地址
- `fileServer`: 静态文件服务
- `dohServer`: DNS-over-HTTPS 服务

### 支持的协议格式
- TCP: `127.0.0.1:40000`
- Unix domain socket: `unix:/path/to/socket`
- H2C: `h2c://localhost:40002`
- Proxy Protocol: `127.0.0.1:40001;proxyProtocol`

## 依赖项

### 主要依赖
- `github.com/caddyserver/certmagic`: 自动证书管理
- `github.com/go-acme/lego/v3`: ACME 协议实现
- `github.com/miekg/dns`: DNS 库
- `github.com/stevenjohnstone/sni`: SNI 解析
- `golang.org/x/net`: Go 扩展网络库
- `gopkg.in/yaml.v2`: YAML 解析

### 其他重要依赖
- `github.com/nanmu42/gzip`: Gzip 压缩支持
- `github.com/dvsekhvalnov/jose2go`: JWT/JWE 支持

## 开发约定

### 代码风格
- 使用 Go 标准格式（`go fmt`）
- 错误处理：不忽略错误，使用 log.Printf 记录错误信息
- 并发：使用 goroutine 处理并发连接
- 资源管理：使用 defer 确保连接和资源正确关闭

### Handler 接口
所有流量处理器必须实现 `Handler` 接口：

```go
type Handler interface {
    Handle(conn net.Conn)
}
```

### 缓冲区管理
- 使用 `sync.Pool` 管理缓冲区以减少内存分配
- 可通过配置调整入站和出站缓冲区大小

### 测试
项目中未提供测试文件，建议添加单元测试覆盖核心功能。

## 常见问题排查

1. **服务启动失败**
   - 检查是否有权限绑定端口
   - 使用 `setcap` 赋予 CAP_NET_BIND_SERVICE 权限

2. **证书加载失败**
   - 检查证书文件路径和权限
   - 确保 tls-shunt-proxy 用户有读取证书的权限

3. **配置文件错误**
   - Web UI 提供 YAML 语法校验
   - 保存配置后服务会自动重启

## Web UI 配置重载机制

Web UI 的 `/save` 端点实现了配置热重载：
1. 验证 YAML 语法
2. 原子写入配置文件（使用临时文件 + rename）
3. 启动新进程
4. 退出当前进程

这种方式确保了配置变更的安全性和原子性。

## 安全特性

1. **TLS 支持**: 强制使用 TLS 1.2+，仅支持安全的加密套件
2. **Basic Auth**: Web UI 使用 Basic Auth 保护
3. **证书管理**: 支持 Let's Encrypt 自动证书管理
4. **SNI 隔离**: 不同虚拟主机使用独立配置

## 使用场景

- VMESS over TLS 分流
- Trojan 协议代理
- 多域名共享 443 端口
- HTTP/HTTPS 服务部署
- 静态网站托管
- WebSocket 代理
- DNS-over-HTTPS 服务

## 注意事项

1. 启用 managedcert 时必须监听 443 端口
2. TLS 1.2 仅支持 FS 且 AEAD 的加密套件
3. Web UI 默认绑定到 127.0.0.1，仅允许本地访问
4. 代理转发使用缓冲区池优化性能
5. 配置修改后需要通过 Web UI 保存以触发重启

## 相关资源

- **Telegram**: https://t.me/tls_shunt_proxy
- **GitHub**: https://github.com/liberal-boy/tls-shunt-proxy
- **版本**: 0.9.3