# TLS 分流器
[Telegram](https://t.me/tls_shunt_proxy)

用于分流 TLS 流量，适用于 vmess + TLS + Web 方案实现，并可以与 trojan 共享端口。
* sni 分流
* http 和无特征流量分流
* 静态网站服务器
* 自动获取证书

## 下载安装
对于 linux-amd64 可以使用脚本安装，以 root 身份执行以下命令
```shell script
bash <(curl -L -s https://raw.githubusercontent.com/liberal-boy/tls-shunt-proxy/master/dist/install.sh)
```
* 配置文件位于 `/etc/tls-shunt-proxy/config.yaml`

* 其他平台需自行编译安装

## 使用
命令行参数：
```
  -config string
        Path to config file (default "./config.yaml")
```

### Web 管理界面

tls-shunt-proxy 内置了基于 Web 的配置管理界面，方便在线编辑和管理配置文件。

#### 访问 Web UI

服务启动后，Web UI 会自动在 `127.0.0.1:8080` 启动。

访问地址：`http://127.0.0.1:8080`

#### 默认登录凭据

- 用户名：`admin`
- 密码：`admin`

#### 自定义登录凭据

可以通过环境变量自定义 Web UI 的登录凭据：

```bash
export WEBUI_USER=your_username
export WEBUI_PASS=your_password
./tls-shunt-proxy -config /path/to/config.yaml
```

#### Web UI 功能

1. **在线配置编辑器**：直接在浏览器中编辑 YAML 配置文件
2. **配置验证**：保存前自动验证 YAML 语法和业务逻辑
3. **一键保存与重启**：保存配置后自动重启服务以应用更改
4. **连接统计**：查看当前活跃连接数
5. **配置生成向导**：支持通过向导方式生成配置文件

#### API 接口

Web UI 提供了以下 RESTful API 接口：

| 接口 | 方法 | 描述 |
|------|------|------|
| `/api/config` | GET | 获取当前配置文件内容 |
| `/save` | POST | 保存配置并重启服务 |
| `/api/reload` | POST | 零停机重载配置 |
| `/api/stats` | GET | 获取连接统计信息 |
| `/api/validate-cloudflare` | POST | 验证 Cloudflare API 凭据 |
| `/api/generate-config` | POST | 生成配置文件（旧版向导） |
| `/api/generate-strategy-config` | POST | 使用策略模式生成配置 |
| `/api/save-strategy-config` | POST | 保存策略生成的配置 |

### 配置重载

tls-shunt-proxy 支持两种配置重载方式：

#### 方式一：通过 Web UI 重载（推荐）

1. 访问 Web UI：`http://127.0.0.1:8080`
2. 编辑配置文件
3. 点击"保存"按钮
4. 系统会自动验证配置并重启服务

#### 方式二：零停机重载

对于需要保证现有连接不中断的场景，可以使用零停机重载功能：

```bash
# 通过 Web UI API 触发零停机重载
curl -X POST http://127.0.0.1:8080/api/reload \
  -u admin:admin
```

零停机重载的特点：
- 现有连接不会中断
- 新进程启动后，旧进程优雅关闭
- 适用于证书更新、监听地址变更等场景

#### 方式三：手动重启

```bash
# 停止服务
pkill -f tls-shunt-proxy

# 启动服务
./tls-shunt-proxy -config /path/to/config.yaml
```

### 部署和更新

#### 本地编译

```bash
# macOS 本地编译
go build -o tls-shunt-proxy main.go

# 交叉编译为 Linux AMD64
GOOS=linux GOARCH=amd64 go build -o tls-shunt-proxy-linux-amd64 main.go

# 交叉编译为 Linux ARM64
GOOS=linux GOARCH=arm64 go build -o tls-shunt-proxy-linux-arm64 main.go
```

#### 使用脚本部署到 Linux 服务器

项目提供了自动化部署脚本，支持 Ubuntu、Debian、CentOS 等基于 systemd 的 Linux 发行版。

```bash
# 克隆项目
git clone https://github.com/liberal-boy/tls-shunt-proxy.git
cd tls-shunt-proxy

# 运行部署脚本（需要 root 权限）
sudo bash tls-proxy-config/scripts/deploy.sh
```

部署脚本会自动完成以下操作：
1. 安装必要的依赖
2. 编译 tls-shunt-proxy
3. 创建 systemd 服务
4. 配置防火墙规则
5. 启动服务

#### 手动部署

如果需要手动部署到生产服务器：

```bash
# 1. 编译生成 Linux 二进制文件
GOOS=linux GOARCH=amd64 go build -o tls-shunt-proxy-linux-amd64 main.go

# 2. 上传文件到服务器
scp tls-shunt-proxy-linux-amd64 user@server:/opt/tls-shunt-proxy/tls-shunt-proxy
scp -r webui user@server:/opt/tls-shunt-proxy/
scp config.yaml user@server:/etc/tls-shunt-proxy/config.yaml

# 3. 在服务器上设置权限
ssh user@server
sudo chmod +x /opt/tls-shunt-proxy/tls-shunt-proxy
sudo systemctl stop tls-shunt-proxy
sudo systemctl start tls-shunt-proxy

# 4. 检查服务状态
sudo systemctl status tls-shunt-proxy
```

#### 热重载配置

tls-shunt-proxy 支持通过环境变量配置热重载参数：

| 环境变量 | 说明 | 默认值 |
|---------|------|--------|
| `TLS_SHUNT_NEW_PROCESS_WAIT_TIME` | 新进程启动等待时间（秒） | 3 |
| `TLS_SHUNT_GRACEFUL_TIMEOUT` | 优雅关闭超时时间（秒） | 60 |

```bash
# 自定义热重载参数
export TLS_SHUNT_NEW_PROCESS_WAIT_TIME=5
export TLS_SHUNT_GRACEFUL_TIMEOUT=120
./tls-shunt-proxy -config /path/to/config.yaml
```

#### systemd 服务管理

如果使用 systemd 管理服务，可以使用以下命令：

```bash
# 启动服务
sudo systemctl start tls-shunt-proxy

# 停止服务
sudo systemctl stop tls-shunt-proxy

# 重启服务
sudo systemctl restart tls-shunt-proxy

# 查看服务状态
sudo systemctl status tls-shunt-proxy

# 查看日志
sudo journalctl -u tls-shunt-proxy -f

# 开机自启
sudo systemctl enable tls-shunt-proxy

# 禁用开机自启
sudo systemctl disable tls-shunt-proxy
```

#### 服务权限配置

如果服务启动失败，提示无法绑定端口（80/443），需要赋予 CAP_NET_BIND_SERVICE 权限：

```bash
# 赋予绑定特权端口的权限
sudo setcap "cap_net_bind_service=+ep" /usr/local/bin/tls-shunt-proxy

# 或者以 root 用户运行服务
sudo /usr/local/bin/tls-shunt-proxy -config /etc/tls-shunt-proxy/config.yaml
```

#### 配置文件路径

- **默认路径**：`./config.yaml`（程序运行目录）
- **Linux 标准路径**：`/etc/tls-shunt-proxy/config.yaml`
- **自定义路径**：通过 `-config` 参数指定

```bash
# 使用自定义配置文件
./tls-shunt-proxy -config /path/to/custom/config.yaml
```

<details>
  <summary>点击此处展开示例配置文件</summary>
  
```yml
# listen: 监听地址
listen: 0.0.0.0:443

# redirecthttps: 监听一个地址，发送到这个地址的 http 请求将被重定向到 https
redirecthttps: 0.0.0.0:80

# inboundbuffersize: 入站缓冲区大小，单位 KB, 默认值 4
# 相同吞吐量和连接数情况下，缓冲区越大，消耗的内存越大，消耗 CPU 时间越少。在网络吞吐量较低时，缓存过大可能增加延迟。
inboundbuffersize: 4

# outboundbuffersize: 出站缓冲区大小，单位 KB, 默认值 32
outboundbuffersize: 32

# 无法识别 sni 或 sni 不在 vhost 中的请求回落地址，同 proxyPass 参数格式
fallback: 127.0.0.1:8443

# vhosts: 按照按照 tls sni 扩展划分为多个虚拟 host
vhosts:

    # name 对应 tls sni 扩展的 server name
  - name: vmess.example.com

    # tlsoffloading: 解开 tls，true 为解开，解开后可以识别 http 流量，适用于 vmess over tls 和 http over tls (https) 分流等
    tlsoffloading: true

    # managedcert: 管理证书，开启后将自动从 LetsEncrypt 获取证书，根据 LetsEncrypt 的要求，必须监听 443 端口才能签发
    # 开启时 cert 和 key 设置的证书无效，关闭时将使用 cert 和 key 设置的证书
    managedcert: false

    # keytype: 启用 managedcert 时，生成的密钥对类型，支持的选项 ed25519、p256、p384、rsa2048、rsa4096、rsa8192
    keytype: p256

    # cert: tls 证书路径，
    cert: /etc/ssl/vmess.example.com.pem

    # key: tls 私钥路径
    key: /etc/ssl/vmess.example.com.key

    # alpn: ALPN, 多个 next protocol 之间用 "," 分隔
    alpn: h2,http/1.1

    # protocols: 指定 tls 协议版本，格式为 min,max , 可用值 tls12(默认最小), tls13(默认最大)
    # 如果最小值和最大值相同，那么你只需要写一次
    # tls12 仅支持 FS 且 AEAD 的加密套件
    protocols: tls12,tls13

    # http: 识别出的 http 流量的处理方式
    http:

      # paths: 按 http 请求的 path 分流，从上到下匹配，找不到匹配项则使用 http 的 handler
      paths:

          # path: path 以该字符串开头的请求将应用此 handler
        - path: /vmess/ws/
          handler: proxyPass
          args: 127.0.0.1:40000

          # path: http/2 请求的 path 将被识别为 *
        - path: "*"
          handler: proxyPass
          args: 127.0.0.1:40003

        - path: /static/

          # trimprefix: 修剪前缀，将 http 流量交给 handler 时，修剪 path 中的前缀
          # 如将 /static/logo.jpg 修剪为 /logo.jpg
          trimprefix: /static

          handler: fileServer
          args: /var/www/static

      # handler: fileServer 将服务一个静态网站
      # fileServer 支持 h2c, 如果使用 fileServer 处理 http, 且未设置 paths, alpn 可以开启 h2
      handler: fileServer

      # args: 静态网站的文件路径
      args: /var/www/html
      
    # http/2 请求的处理方式，当此项设置后，http 中的 path: "*" 设置将无效
    http2:
      - path: /
        handler: fileServer
        args: /var/www/rayfantasy
      - path: /vmess
        handler: proxyPass
        # 目前只支持目标接受 h2c
        args: h2c://localhost:40002

    # trojan: Trojan 协议流量处理方式
    trojan:
      handler: proxyPass
      args: 127.0.0.1:4430

    # default: 其他流量处理方式
    default:

      # handler: proxyPass 将流量转发至另一个地址
      handler: proxyPass

      # args: 转发的目标地址
      args: 127.0.0.1:40001

      # args: 支持通过 Proxy Protocol 将源地址向后端传抵，目前仅支持 v1
      # args: 127.0.0.1:40001;proxyProtocol

      # args: 也可以使用 domain socket
      # args: unix:/path/to/ds/file

  - name: trojan.example.com

    # tlsoffloading: 解开 tls，false 为不解开，直接处理 tls 流量，适用于 trojan-gfw 等
    tlsoffloading: false

    # default: 关闭 tlsoffloading 时，目前没有识别方法，均按其他流量处理
    default:
      handler: proxyPass
      args: 127.0.0.1:8443
```
</details>

## 故障排查和常见问题

1. service 启动失败，请使用命令 `sudo setcap "cap_net_bind_service=+ep" /usr/local/bin/tls-shunt-proxy` 给 tls-shunt-proxy 赋予 CAP_NET_BIND_SERVICE 的 capability ，然后 `sudo -u tls-shunt-proxy /usr/local/bin/tls-shunt-proxy -config /etc/tls-shunt-proxy/config.yaml` 运行，获取错误信息

2. `fail to load tls key pair for xxx.xxx: open /xxx/xxx.key: permission denied` 确保用户 `tls-shunt-proxy` 有权读取证书




## 1、需要支持通配
  - name: game{{ 1 }}.elembeast.com
    tlsoffloading: true
    managedcert: true
    alpn: http/1.1
    protocols: tls12,tls13
    http:
      handler: proxyPass
      args: 127.0.0.1:8{{ 1 }}
    default:
      handler: proxyPass
      args: 127.0.0.1:8{{ 1 }}


## 2、设置证书提前N天开始申请新证书
具体方案见：CERTIFICATE_ANALYSIS.md


## 3、高可用方案见 HIGH_AVAILABILITY 目录


# MAP_RODE
- 修改域名证书相关管理