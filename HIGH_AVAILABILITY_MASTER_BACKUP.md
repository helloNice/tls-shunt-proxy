# tls-shunt-proxy 主备模式部署方案

## 问题分析

在同一台主机上，只有一个进程可以监听 443 端口。因此主备模式需要**两台独立的主机**。

---

## 方案一：两台独立主机 + Keepalived（推荐）

### 架构设计
```
                    ┌─────────────┐
                    │   客户端请求 │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   VIP (虚拟IP)│
                    │   192.168.1.100│
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
    ┌────▼────┐      ┌────▼────┐
    │ Node 1  │      │ Node 2  │
    │  (主)   │      │  (备)   │
    │ 192.168.1.101 │ 192.168.1.102
    │         │      │         │
    │ tls-shunt│      │ tls-shunt│
    │ -proxy  │      │ -proxy  │
    │ :443    │      │ :443    │
    └─────────┘      └─────────┘
         │                 │
         └─────────────────┼─────────────────┘
                           │
                    ┌──────▼──────┐
                    │  后端服务池  │
                    │  (共享访问)  │
                    └─────────────┘
```

### 硬件要求
- **两台独立的服务器**（虚拟机或物理机）
- 同一网段，可以互通
- 共享后端服务访问

### 实施步骤

#### 1. 安装 Keepalived

**Node 1 (主节点)**
```bash
# CentOS/RHEL
sudo yum install keepalived -y

# Ubuntu/Debian
sudo apt-get install keepalived -y
```

**Node 2 (备节点)**
```bash
# 同上安装
```

#### 2. 配置 Keepalived

**Node 1 (主节点) - /etc/keepalived/keepalived.conf**
```conf
! Configuration File for keepalived

global_defs {
    router_id LVS_DEVEL
}

# 健康检查脚本
vrrp_script chk_tls_shunt_proxy {
    script "/usr/local/bin/check_tls_shunt_proxy.sh"
    interval 2
    weight -20
    fall 3
    rise 2
}

vrrp_instance VI_1 {
    state MASTER           # 主节点状态
    interface eth0         # 网卡名称（根据实际情况修改）
    virtual_router_id 51   # VRID，两台主机必须相同
    priority 100           # 优先级，主节点更高
    advert_int 1           # 发送通告间隔（秒）

    authentication {
        auth_type PASS
        auth_pass secret   # 认证密码
    }

    virtual_ipaddress {
        192.168.1.100     # VIP 虚拟IP
    }

    track_script {
        chk_tls_shunt_proxy
    }

    # 状态切换通知
    notify_master "/usr/local/bin/notify.sh master"
    notify_backup "/usr/local/bin/notify.sh backup"
    notify_fault "/usr/local/bin/notify.sh fault"
}
```

**Node 2 (备节点) - /etc/keepalived/keepalived.conf**
```conf
! Configuration File for keepalived

global_defs {
    router_id LVS_DEVEL
}

vrrp_script chk_tls_shunt_proxy {
    script "/usr/local/bin/check_tls_shunt_proxy.sh"
    interval 2
    weight -20
    fall 3
    rise 2
}

vrrp_instance VI_1 {
    state BACKUP          # 备节点状态
    interface eth0         # 网卡名称
    virtual_router_id 51   # VRID，必须与主节点相同
    priority 50            # 优先级，低于主节点
    advert_int 1

    authentication {
        auth_type PASS
        auth_pass secret   # 必须与主节点相同
    }

    virtual_ipaddress {
        192.168.1.100     # VIP，必须与主节点相同
    }

    track_script {
        chk_tls_shunt_proxy
    }

    notify_master "/usr/local/bin/notify.sh master"
    notify_backup "/usr/local/bin/notify.sh backup"
    notify_fault "/usr/local/bin/notify.sh fault"
}
```

#### 3. 创建健康检查脚本

**两台节点都需要创建：/usr/local/bin/check_tls_shunt_proxy.sh**
```bash
#!/bin/bash

# 检查 tls-shunt-proxy 进程是否运行
if ! pgrep -f "tls-shunt-proxy" > /dev/null; then
    echo "tls-shunt-proxy process not running"
    exit 1
fi

# 检查端口是否监听
if ! netstat -tuln | grep -q ":443 "; then
    echo "Port 443 not listening"
    exit 1
fi

# 检查健康检查端点（如果已实现）
# curl -f http://127.0.0.1:8080/health || exit 1

exit 0
```

```bash
# 赋予执行权限
sudo chmod +x /usr/local/bin/check_tls_shunt_proxy.sh
```

#### 4. 创建通知脚本（可选）

**两台节点都需要创建：/usr/local/bin/notify.sh**
```bash
#!/bin/bash

TYPE=$1

case $TYPE in
    master)
        echo "Becoming MASTER"
        # 可以在这里执行主节点启动的操作
        # 例如：启动某些服务、发送通知等
        ;;
    backup)
        echo "Becoming BACKUP"
        # 可以在这里执行备节点启动的操作
        ;;
    fault)
        echo "FAULT state"
        # 可以在这里执行故障处理操作
        ;;
esac
```

```bash
sudo chmod +x /usr/local/bin/notify.sh
```

#### 5. 部署 tls-shunt-proxy

**两台节点都需要部署：**

```bash
# 1. 复制可执行文件
sudo cp tls-shunt-proxy /usr/local/bin/
sudo chmod +x /usr/local/bin/tls-shunt-proxy

# 2. 创建配置目录
sudo mkdir -p /etc/tls-shunt-proxy

# 3. 复制配置文件
sudo cp config.yaml /etc/tls-shunt-proxy/

# 4. 创建证书目录
sudo mkdir -p /etc/tls-shunt-proxy/certificates

# 5. 创建 systemd 服务文件
sudo tee /etc/systemd/system/tls-shunt-proxy.service > /dev/null <<EOF
[Unit]
Description=TLS Shunt Proxy
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/etc/tls-shunt-proxy
ExecStart=/usr/local/bin/tls-shunt-proxy -config /etc/tls-shunt-proxy/config.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

# 6. 启动服务
sudo systemctl daemon-reload
sudo systemctl enable tls-shunt-proxy
sudo systemctl start tls-shunt-proxy
```

#### 6. 同步配置和证书

**方案 A：使用 rsync 定时同步**

在主节点设置定时任务：
```bash
# 编辑 crontab
crontab -e

# 每 5 分钟同步一次配置
*/5 * * * * rsync -avz --delete /etc/tls-shunt-proxy/ root@192.168.1.102:/etc/tls-shunt-proxy/

# 每 5 分钟同步一次证书
*/5 * * * * rsync -avz --delete ./certificates/ root@192.168.1.102:./certificates/
```

**方案 B：使用共享存储（推荐）**

```bash
# 在两台节点上挂载 NFS 共享存储
sudo mkdir -p /etc/tls-shunt-proxy/shared
sudo mount -t nfs nfs-server:/tls-shunt-proxy /etc/tls-shunt-proxy/shared

# 使用软链接
sudo ln -s /etc/tls-shunt-proxy/shared/config.yaml /etc/tls-shunt-proxy/config.yaml
sudo ln -s /etc/tls-shunt-proxy/shared/certificates ./certificates/
```

**方案 C：使用 inotify 实时同步**

在主节点：
```bash
# 安装 lsyncd
sudo apt-get install lsyncd -y  # Ubuntu/Debian
# 或
sudo yum install lsyncd -y       # CentOS/RHEL

# 配置 lsyncd
sudo tee /etc/lsyncd.conf.lua > /dev/null <<EOF
settings {
    logfile = "/var/log/lsyncd.log",
    statusFile = "/var/log/lsyncd-status.log",
    insist = true,
    nodaemon = false
}

sync {
    default.rsync,
    source = "/etc/tls-shunt-proxy/",
    target = "root@192.168.1.102:/etc/tls-shunt-proxy/",
    rsync = {
        archive = true,
        compress = true,
        verbose = true,
        _extra = {"--delete"}
    }
}
EOF

# 启动 lsyncd
sudo systemctl enable lsyncd
sudo systemctl start lsyncd
```

#### 7. 配置防火墙

**两台节点都需要配置：**

```bash
# 开放必要端口
sudo firewall-cmd --permanent --add-port=443/tcp
sudo firewall-cmd --permanent --add-port=80/tcp
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --permanent --add-port=22/tcp  # SSH
sudo firewall-cmd --reload

# 如果使用 iptables
sudo iptables -A INPUT -p tcp --dport 443 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 80 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
sudo service iptables save
```

#### 8. 启动服务

**两台节点：**

```bash
# 启动 Keepalived
sudo systemctl enable keepalived
sudo systemctl start keepalived

# 启动 tls-shunt-proxy
sudo systemctl start tls-shunt-proxy

# 检查状态
sudo systemctl status keepalived
sudo systemctl status tls-shunt-proxy

# 检查 VIP
ip addr show eth0
```

**验证：**

```bash
# 在主节点上应该看到 VIP
# 192.168.1.100 应该在主节点的网卡上

# 测试故障转移
# 在主节点停止 tls-shunt-proxy
sudo systemctl stop tls-shunt-proxy

# VIP 应该自动漂移到备节点
# 在备节点上检查：ip addr show eth0
```

### 工作原理

1. **正常运行时**
   - VIP 绑定在主节点（Node 1）
   - 所有流量通过 VIP 访问主节点
   - 主节点处理所有请求
   - 备节点处于待机状态

2. **故障转移**
   - 主节点故障（进程崩溃、服务器宕机等）
   - Keepalived 健康检查失败
   - 自动将 VIP 漂移到备节点（Node 2）
   - 流量自动切换到备节点
   - 故障转移时间通常 < 3 秒

3. **故障恢复**
   - 主节点恢复正常
   - 主节点重新获得 VIP（抢占模式）
   - 流量切回主节点

---

## 方案二：单主机 + 容器化（不推荐）

如果只有一台主机，无法实现真正的高可用。可以考虑：

```
                    ┌─────────────┐
                    │   客户端请求 │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  Docker LB  │
                    │  (Nginx)    │
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
    ┌────▼────┐      ┌────▼────┐
    │ 容器 1  │      │ 容器 2  │
    │ tls-shunt│      │ tls-shunt│
    │ -proxy  │      │ -proxy  │
    │ :8443   │      │ :8444   │
    └─────────┘      └─────────┘
```

**问题：**
- ❌ 单点故障仍然存在
- ❌ 宿主机故障，所有容器都不可用
- ❌ 只能提高可用性，不能消除单点故障

---

## 方案三：云服务商的高可用方案

### 使用云负载均衡器

```
                    ┌─────────────┐
                    │   客户端请求 │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  Cloud LB   │
                    │  (ALB/ELB)  │
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
    ┌────▼────┐      ┌────▼────┐      ┌────▼────┐
    │  EC2-1  │      │  EC2-2  │      │  EC2-N  │
    │         │      │         │      │         │
    │ tls-shunt│      │ tls-shunt│      │ tls-shunt│
    │ -proxy  │      │ -proxy  │      │ -proxy  │
    │ :443    │      │ :443    │      │ :443    │
    └─────────┘      └─────────┘      └─────────┘
```

### AWS 部署示例

```bash
# 1. 创建 EC2 实例（至少 2 台）
# 2. 在每台 EC2 上部署 tls-shunt-proxy
# 3. 创建 Application Load Balancer (ALB)
# 4. 配置目标组，将 EC2 实例添加到目标组
# 5. 配置监听器：443 端口 → 目标组
# 6. 配置健康检查
# 7. 配置域名解析到 ALB 的 DNS 名称
```

### 阿里云部署示例

```bash
# 1. 创建 ECS 实例（至少 2 台）
# 2. 在每台 ECS 上部署 tls-shunt-proxy
# 3. 创建负载均衡实例（SLB/ALB）
# 4. 添加后端服务器
# 5. 配置监听和健康检查
# 6. 配置域名解析到负载均衡的公网 IP
```

---

## 监控和告警

### 监控指标

```bash
# 1. Keepalived 状态
sudo systemctl status keepalived

# 2. VIP 状态
ip addr show eth0 | grep 192.168.1.100

# 3. tls-shunt-proxy 状态
sudo systemctl status tls-shunt-proxy

# 4. 日志查看
sudo journalctl -u keepalived -f
sudo journalctl -u tls-shunt-proxy -f
```

### 告警配置

```bash
# 使用 Prometheus + Alertmanager 监控
# 监控项：
# - Keepalived 状态变化
# - VIP 漂移事件
# - tls-shunt-proxy 进程状态
# - 端口监听状态
# - 健康检查失败
```

---

## 故障排查

### 常见问题

**1. VIP 无法绑定**
```bash
# 检查网卡名称
ip addr show

# 检查配置文件中的 interface 名称
sudo cat /etc/keepalived/keepalived.conf

# 检查防火墙规则
sudo iptables -L -n
```

**2. 健康检查失败**
```bash
# 手动运行健康检查脚本
sudo /usr/local/bin/check_tls_shunt_proxy.sh

# 检查进程状态
ps aux | grep tls-shunt-proxy

# 检查端口监听
sudo netstat -tuln | grep 443
```

**3. VIP 无法漂移**
```bash
# 检查两台节点的配置是否一致
diff /etc/keepalived/keepalived.conf <(ssh root@192.168.1.102 cat /etc/keepalived/keepalived.conf)

# 检查网络连通性
ping 192.168.1.102

# 检查组播通信
sudo tcpdump -i eth0 vrrp
```

---

## 总结

### 推荐方案

| 场景 | 推荐方案 | 硬件要求 |
|------|----------|----------|
| **本地环境** | 两台主机 + Keepalived | 2 台独立服务器 |
| **云环境（AWS）** | ALB + 多实例 | 2+ EC2 实例 |
| **云环境（阿里云）** | SLB/ALB + 多实例 | 2+ ECS 实例 |
| **单机测试** | 无法实现真正高可用 | 不适用 |

### 关键要点

1. **需要两台独立主机** - 无法在同一主机上实现主备
2. **VIP 漂移机制** - 自动故障转移
3. **配置同步** - 保持主备节点配置一致
4. **健康检查** - 及时发现故障
5. **监控告警** - 及时响应故障

### 成本估算

- **硬件成本**：2 台服务器
- **网络成本**：同一网段
- **软件成本**：免费（Keepalived）
- **运维成本**：中等

### 实施时间

- 准备工作：1-2 天
- 部署实施：1-2 天
- 测试验证：1 天
- **总计**：3-5 天