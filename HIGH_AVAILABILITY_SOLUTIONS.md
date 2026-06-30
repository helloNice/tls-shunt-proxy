# tls-shunt-proxy 高可用方案建议

## 当前系统架构分析

### 现状
- 单进程部署，监听单一端口（默认 443）
- 无状态设计，所有配置存储在本地文件
- 证书存储在本地文件系统
- 无内置的健康检查和故障转移机制
- Web UI 绑定到 127.0.0.1，仅支持本地访问

### 单点故障风险
1. **进程崩溃** - 服务不可用
2. **服务器故障** - 完全中断
3. **证书过期** - TLS 握手失败
4. **配置错误** - 启动失败
5. **网络问题** - 无法访问后端服务

---

## 高可用方案

### 方案一：负载均衡 + 多实例部署（推荐）

#### 架构设计
```
                    ┌─────────────┐
                    │   DNS / CDN │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   L4/L7 LB  │ (Nginx/HAProxy/云LB)
                    │  (Keepalived│ (VIP漂移)
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
    ┌────▼────┐      ┌────▼────┐      ┌────▼────┐
    │ Node 1  │      │ Node 2  │      │ Node N  │
    │ tls-shunt│      │ tls-shunt│      │ tls-shunt│
    │ -proxy  │      │ -proxy  │      │ -proxy  │
    └────┬────┘      └────┬────┘      └────┬────┘
         │                 │                 │
         └─────────────────┼─────────────────┘
                           │
                    ┌──────▼──────┐
                    │  后端服务池  │
                    └─────────────┘
```

#### 实施要点

**1. 配置共享**
```yaml
# 使用共享配置存储
- 方案A: NFS/GlusterFS 共享配置目录
- 方案B: 配置中心 (Consul/Etcd/Redis)
- 方案C: GitOps 方式，通过 webhook 同步配置
```

**2. 证书共享**
```yaml
# 证书存储策略
- 方案A: 共享存储（NFS/S3）
- 方案B: 使用外部 ACME 服务（如 cert-manager）
- 方案C: 主节点获取证书，同步到其他节点
```

**3. 健康检查**
```yaml
# 健康检查端点（需实现）
- /health: 基础健康检查
- /ready: 就绪检查
- /certificates: 证书状态检查
```

**4. 无状态设计优化**
- 配置热重载（已有）
- 会话保持（可选，根据业务需求）
- 无后端状态依赖

#### 优点
✅ 高可用性，单节点故障不影响服务
✅ 水平扩展，支持大量并发
✅ 负载均衡，提升整体性能
✅ 滚动更新，零停机部署

#### 缺点
❌ 需要额外的负载均衡器
❌ 配置和证书同步复杂度增加
❌ 运维成本增加

---

### 方案二：主备模式（Keepalived + VRRP）

#### 架构设计
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
    │ Master  │      │ Backup  │
    │  (主)   │      │  (备)   │
    │ 优先级: 100    │ 优先级: 50
    └────┬────┘      └────┬────┘
         │                 │
    绑定 VIP            监听 VIP
```

#### 实施要点

**1. Keepalived 配置示例**
```conf
# Master 节点
vrrp_instance VI_1 {
    state MASTER
    interface eth0
    virtual_router_id 51
    priority 100
    advert_int 1
    authentication {
        auth_type PASS
        auth_pass secret
    }
    virtual_ipaddress {
        192.168.1.100
    }
    track_script {
        chk_tls_shunt_proxy
    }
}

# 健康检查脚本
vrrp_script chk_tls_shunt_proxy {
    script "/usr/local/bin/check_tls_shunt_proxy.sh"
    interval 2
    weight -20
}
```

**2. 配置同步**
```bash
# 使用 rsync 实时同步配置
rsync -avz --delete /etc/tls-shunt-proxy/ backup-node:/etc/tls-shunt-proxy/

# 或使用 unison 双向同步
unison /etc/tls-shunt-proxy/ ssh://backup-node//etc/tls-shunt-proxy/
```

**3. 证书同步**
```bash
# 证书目录同步
rsync -avz ./certificates/ backup-node:./certificates/

# 或使用共享存储
mount -t nfs nfs-server:/tls-shunt-certificates ./certificates/
```

#### 优点
✅ 实现简单，成本低
✅ 故障切换快（秒级）
✅ 无需额外的负载均衡器
✅ 适合中小规模部署

#### 缺点
❌ 备节点资源闲置
❌ 无法水平扩展
❌ 配置同步需要额外机制
❌ 单主节点，性能受限

---

### 方案三：容器化 + Kubernetes 部署

#### 架构设计
```
                    ┌─────────────┐
                    │ Ingress/Gateway│
                    │ (Traefik/Nginx)│
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  Kubernetes │
                    │   Service   │
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
    ┌────▼────┐      ┌────▼────┐      ┌────▼────┐
    │  Pod 1  │      │  Pod 2  │      │  Pod N  │
    └────┬────┘      └────┬────┘      └────┬────┘
         │                 │                 │
    ┌────▼────┐      ┌────▼────┐      ┌────▼────┐
    │ConfigMap│      │ConfigMap│      │ConfigMap│
    │  Secret │      │  Secret │      │  Secret │
    └─────────┘      └─────────┘      └─────────┘
```

#### 实施要点

**1. 配置管理**
```yaml
# ConfigMap 存储配置
apiVersion: v1
kind: ConfigMap
metadata:
  name: tls-shunt-proxy-config
data:
  config.yaml: |
    listen: 0.0.0.0:443
    vhosts:
      - name: example.com
        ...
```

**2. 证书管理**
```yaml
# Secret 存储证书
apiVersion: v1
kind: Secret
metadata:
  name: tls-shunt-proxy-certs
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-cert>
  tls.key: <base64-encoded-key>

# 或使用 cert-manager 自动管理
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: tls-shunt-proxy-cert
spec:
  secretName: tls-shunt-proxy-certs
  dnsNames:
    - example.com
  issuerRef:
    name: letsencrypt-prod
```

**3. Deployment 配置**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tls-shunt-proxy
spec:
  replicas: 3
  selector:
    matchLabels:
      app: tls-shunt-proxy
  template:
    metadata:
      labels:
        app: tls-shunt-proxy
    spec:
      containers:
      - name: tls-shunt-proxy
        image: tls-shunt-proxy:latest
        ports:
        - containerPort: 443
        volumeMounts:
        - name: config
          mountPath: /etc/tls-shunt-proxy
        - name: certs
          mountPath: /etc/ssl/certs
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: tls-shunt-proxy-config
      - name: certs
        secret:
          secretName: tls-shunt-proxy-certs
```

#### 优点
✅ 原生支持高可用和自动扩缩容
✅ 配置和证书管理标准化
✅ 滚动更新和回滚
✅ 健康检查和自愈
✅ 资源利用率高

#### 缺点
❌ 学习曲线陡峭
❌ 需要完整的 K8s 环境
❌ 运维复杂度高
❌ 适合云原生环境

---

### 方案四：云负载均衡器（云服务商方案）

#### 架构设计
```
                    ┌─────────────┐
                    │  用户域名    │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ Cloud DNS  │
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
    │  EC2    │      │  ECS    │      │  VM     │
    │ Node 1  │      │ Node 2  │      │ Node N  │
    └─────────┘      └─────────┘      └─────────┘
```

#### 实施要点

**1. 使用云服务商提供的 LB**
- AWS: ALB/ELB/CLB
- 阿里云: SLB/ALB
- 腾讯云: CLB
- Cloudflare: Cloudflare Load Balancer

**2. 证书管理**
```yaml
# 使用云服务商的证书服务
- AWS: ACM (AWS Certificate Manager)
- 阿里云: SSL 证书服务
- Cloudflare: Origin Certificates
```

**3. 自动扩缩容**
```yaml
# 基于指标自动扩缩容
- CPU 使用率
- 内存使用率
- 请求数/秒
- 自定义指标
```

#### 优点
✅ 托管服务，运维成本低
✅ 原生支持高可用
✅ 自动扩缩容
✅ 集成监控和告警
✅ 全球部署支持

#### 缺点
❌ 云服务商锁定
❌ 成本随流量增加
❌ 配置灵活性受限
❌ 依赖云服务商

---

## 推荐方案选择

### 场景 1：中小规模（< 1000 QPS）
**推荐：方案二（主备模式）**
- 成本低
- 实现简单
- 维护容易

### 场景 2：中大规模（1000-10000 QPS）
**推荐：方案一（负载均衡 + 多实例）**
- 性能好
- 扩展性强
- 性价比高

### 场景 3：大规模（> 10000 QPS）+ 云原生
**推荐：方案三（Kubernetes）**
- 自动化管理
- 高可用性
- 滚动更新

### 场景 4：云环境部署
**推荐：方案四（云负载均衡器）**
- 托管服务
- 集成度高
- 运维简单

---

## 实施建议

### 短期改进（1-2周）
1. 添加健康检查端点
2. 实现配置热重载优化
3. 添加监控和日志

### 中期改进（1-2月）
1. 实现主备模式
2. 配置和证书同步机制
3. 监控告警系统

### 长期改进（3-6月）
1. 容器化部署
2. Kubernetes 迁移
3. 云原生架构改造

---

## 需要实现的功能

### 1. 健康检查端点
```go
// 需要添加到 webui/server.go
mux.HandleFunc("/health", healthCheckHandler)
mux.HandleFunc("/ready", readinessCheckHandler)
```

### 2. 证书状态查询
```go
// 需要添加到 webui/server.go
mux.HandleFunc("/api/certificates", certificatesStatusHandler)
```

### 3. 配置同步接口
```go
// 支持从远程拉取配置
mux.HandleFunc("/api/config/sync", configSyncHandler)
```

### 4. 优雅关闭
```go
// 监听 SIGTERM/SIGINT 信号
// 等待现有连接处理完成
// 释放资源
```

### 5. 监控指标
```go
// 暴露 Prometheus 指标
mux.HandleFunc("/metrics", metricsHandler)
```

---

## 监控指标建议

### 基础指标
- 连接数（活跃/总连接数）
- 请求速率（RPS/QPS）
- 响应时间（P50/P95/P99）
- 错误率

### TLS 相关
- TLS 握手成功率
- 证书过期时间
- 证书续期状态
- SNI 匹配率

### 后端相关
- 后端健康状态
- 后端响应时间
- 后端错误率

### 资源相关
- CPU 使用率
- 内存使用率
- 网络带宽
- 文件描述符

---

## 告警规则建议

### 严重告警
- 服务不可用（> 1分钟）
- 证书即将过期（< 7天）
- 错误率过高（> 5%）
- 后端不可用（> 30秒）

### 警告告警
- 证书即将过期（< 30天）
- 响应时间过长（P99 > 1s）
- 连接数过高（> 阈值）
- CPU/内存使用率过高（> 80%）

### 信息告警
- 配置已更新
- 证书已续期
- 节点上线/下线