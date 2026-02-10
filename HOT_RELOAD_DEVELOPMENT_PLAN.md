# 不停服重启配置功能开发计划

## 功能概述
实现配置文件热重载功能，在不中断服务的情况下重新加载配置文件，确保无缝切换。

## 技术方案

### 核心设计思路
1. **信号驱动**：使用 SIGHUP 信号触发配置重载
2. **原子切换**：新配置验证通过后原子性替换旧配置
3. **连接隔离**：新旧连接使用各自的配置版本
4. **优雅降级**：配置验证失败时保留旧配置

### 架构设计
```
┌─────────────┐
│   Signal    │ SIGHUP
│   Handler   │─────────┐
└─────────────┘         │
                        ▼
              ┌──────────────────┐
              │  Reload Config   │
              │  (Validation)    │
              └────────┬─────────┘
                       │
           ┌───────────┴───────────┐
           │                       │
           ▼                       ▼
    ┌────────────┐          ┌────────────┐
    │  Success   │          │   Failed   │
    └─────┬──────┘          └─────┬──────┘
          │                      │
          ▼                      ▼
    ┌────────────┐          ┌────────────┐
    │ Apply New  │          │ Keep Old   │
    │ Config     │          │ Config     │
    └────────────┘          └────────────┘
```

## 开发步骤

### 1. 新增 SIGHUP 信号处理器
**文件**: `main.go`

**任务**:
- 在 main.go 中注册 SIGHUP 信号监听
- 收到信号时触发配置重载流程
- 使用 channel 实现优雅的信号传递

**实现要点**:
```go
signal.Notify(sigChan, syscall.SIGHUP)
go func() {
    for range sigChan {
        log.Println("收到 SIGHUP 信号，开始重新加载配置...")
        if err := reloadConfig(); err != nil {
            log.Printf("配置重载失败: %v", err)
        }
    }
}()
```

**验收**:
- [ ] 可以通过 `kill -HUP <pid>` 触发重载
- [ ] 日志正确记录信号接收
- [ ] 不影响主程序运行

---

### 2. 实现配置重载核心逻辑
**文件**: `config/config.go`

**任务**:
- 创建 `ReloadConfig()` 函数
- 读取并验证新配置文件
- 检查配置语法和有效性
- 应用新配置到运行时状态

**实现要点**:
```go
func (c *Config) Reload(configPath string) error {
    // 1. 读取新配置
    newConfig, err := LoadConfig(configPath)
    if err != nil {
        return fmt.Errorf("读取配置失败: %w", err)
    }

    // 2. 验证配置
    if err := newConfig.Validate(); err != nil {
        return fmt.Errorf("配置验证失败: %w", err)
    }

    // 3. 原子性替换
    c.mu.Lock()
    c.config = newConfig
    c.mu.Unlock()

    // 4. 重新加载通配符证书（如果有）
    if len(newConfig.WildcardCerts) > 0 {
        if err := c.wildcardManager.LoadFromConfig(newConfig.WildcardCerts); err != nil {
            log.Printf("通配符证书重载失败: %v", err)
            // 不返回错误，允许部分失败
        }
    }

    return nil
}
```

**验收**:
- [ ] 配置文件语法错误能正确检测
- [ ] 配置验证失败时保留旧配置
- [ ] 配置重载成功后新连接使用新配置
- [ ] 通配符证书配置能正确重载

---

### 3. 实现优雅的连接处理
**文件**: `main.go`, `handler/conn_listener.go`

**任务**:
- 标记现有连接为"旧连接"
- 新连接使用新配置处理
- 旧连接自然关闭后清理资源
- 确保不会突然中断用户请求

**实现要点**:
```go
type Server struct {
    // 配置版本号
    configVersion uint64
    mu            sync.RWMutex
}

// 每次配置重载时增加版本号
func (s *Server) incrementConfigVersion() {
    s.mu.Lock()
    s.configVersion++
    s.mu.Unlock()
}

// 连接处理时读取当前版本
func (s *Server) getCurrentConfigVersion() uint64 {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.configVersion
}
```

**验收**:
- [ ] 重载期间现有连接正常工作
- [ ] 新连接立即使用新配置
- [ ] 旧连接关闭后无资源泄漏
- [ ] 支持同时存在多个配置版本

---

### 4. 错误处理和回滚机制
**文件**: `config/config.go`

**任务**:
- 配置验证失败时保留旧配置
- 记录详细的错误日志
- 向用户报告重载失败原因
- 提供配置回滚能力

**实现要点**:
```go
func (c *Config) Reload(configPath string) error {
    // 备份当前配置
    oldConfig := c.config

    // 尝试加载新配置
    newConfig, err := LoadConfig(configPath)
    if err != nil {
        return fmt.Errorf("读取配置失败: %w", err)
    }

    // 验证配置
    if err := newConfig.Validate(); err != nil {
        log.Printf("配置验证失败，保留旧配置: %v", err)
        return fmt.Errorf("配置验证失败: %w", err)
    }

    // 应用新配置
    c.mu.Lock()
    c.config = newConfig
    c.mu.Unlock()

    log.Printf("配置重载成功，版本: %d -> %d", oldConfig.version, newConfig.version)
    return nil
}
```

**验收**:
- [ ] 配置验证失败时旧配置正常工作
- [ ] 错误日志包含详细错误信息
- [ ] Web UI 显示重载失败原因
- [ ] 支持手动回滚到旧配置

---

### 5. Web UI 界面增强
**文件**: `webui/server.go`, `webui/templates/index.html`

**任务**:
- 在 Web UI 添加"重新加载配置"按钮
- 显示配置重载状态和结果
- 展示配置验证错误信息
- 添加配置版本对比功能（可选）

**实现要点**:
```go
// Web UI 添加重载端点
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
    if err := config.Releload(configPath); err != nil {
        json.NewEncoder(w).Encode(map[string]interface{}{
            "success": false,
            "error":   err.Error(),
        })
        return
    }

    json.NewEncoder(w).Encode(map[string]interface{}{
        "success": true,
        "message": "配置重载成功",
    })
}
```

**验收**:
- [ ] Web UI 有重载配置按钮
- [ ] 重载成功显示成功消息
- [ ] 重载失败显示详细错误
- [ ] 显示当前配置版本号

---

### 6. 日志和监控增强
**文件**: `main.go`, `config/config.go`

**任务**:
- 记录配置重载事件
- 记录重载前后的配置变更
- 统计新旧连接数
- 提供重载历史查询

**实现要点**:
```go
type ReloadEvent struct {
    Timestamp   time.Time
    OldVersion  uint64
    NewVersion  uint64
    Success     bool
    Error       string
    ConfigDiff  string
}

var reloadHistory []ReloadEvent

func logReloadEvent(event ReloadEvent) {
    reloadHistory = append(reloadHistory, event)
    log.Printf("配置重载事件: %+v", event)
}
```

**验收**:
- [ ] 每次重载都有日志记录
- [ ] 记录配置变更详情
- [ ] 统计活跃连接数
- [ ] 可查询重载历史

---

## 验收标准

### 功能验收
- [ ] 执行 `kill -HUP <pid>` 可触发配置重载
- [ ] Web UI 点击"重新加载配置"按钮成功重载
- [ ] 配置重载期间现有连接不受影响
- [ ] 新连接立即使用新配置
- [ ] 配置验证失败时保留旧配置并记录错误
- [ ] 重载成功后日志记录清晰
- [ ] 支持配置文件路径参数

### 兼容性验收
- [ ] 与现有功能完全兼容
- [ ] 不影响 TLS 连接
- [ ] 不影响通配符证书管理
- [ ] 不影响 Web UI 访问
- [ ] 不影响现有 vhosts 配置
- [ ] 不影响 fallback 配置

### 稳定性验收
- [ ] 连续重载 100 次无崩溃
- [ ] 重载时高并发场景测试通过（1000+ 连接）
- [ ] 无内存泄漏（运行 24 小时）
- [ ] 旧连接正常关闭，无僵尸连接
- [ ] 重载失败不影响服务可用性
- [ ] 配置文件不存在时优雅处理

### 性能验收
- [ ] 重载时间 < 5 秒（小配置，< 10 个 vhosts）
- [ ] 重载时间 < 30 秒（大配置，> 100 个 vhosts）
- [ ] 重载期间 CPU 峰值 < 50%
- [ ] 重载期间内存增加 < 100MB
- [ ] 新连接建立延迟 < 100ms

### 用户体验验收
- [ ] 重载过程对用户透明
- [ ] 错误提示清晰友好
- [ ] Web UI 操作响应及时
- [ ] 提供配置预览功能（可选）
- [ ] 支持一键回滚（可选）

### 安全性验收
- [ ] 配置文件权限检查
- [ ] 敏感信息不在日志中泄露
- [ ] Web UI 重载需要认证
- [ ] 防止重载攻击（频率限制）

---

## 测试用例

### 1. 基本功能测试
```bash
# 测试 1: SIGHUP 信号触发重载
kill -HUP $(pgrep tls-shunt-proxy)
# 预期: 日志显示"收到 SIGHUP 信号，开始重新加载配置..."

# 测试 2: 配置验证失败
# 修改配置文件为非法 YAML
# 执行重载
# 预期: 保留旧配置，日志显示验证错误

# 测试 3: 正常重载
# 修改有效配置
# 执行重载
# 预期: 新连接使用新配置
```

### 2. 连接隔离测试
```bash
# 测试 1: 重载期间现有连接
# 建立连接 -> 重载 -> 发送请求
# 预期: 请求成功，使用旧配置

# 测试 2: 重载后新连接
# 重载 -> 建立连接 -> 发送请求
# 预期: 请求成功，使用新配置

# 测试 3: 旧连接关闭
# 建立连接 -> 重载 -> 等待旧连接关闭
# 预期: 无资源泄漏
```

### 3. 压力测试
```bash
# 测试 1: 高并发重载
# 同时建立 1000 个连接 -> 连续重载 10 次
# 预期: 无崩溃，所有连接正常

# 测试 2: 长时间运行
# 运行 24 小时，每小时重载一次
# 预期: 无内存泄漏，服务稳定

# 测试 3: 错误配置冲击
# 连续重载 100 次错误配置
# 预期: 服务正常运行，旧配置持续生效
```

---

## 风险评估

### 高风险
- **配置验证不完整**: 可能导致服务崩溃
  - **缓解措施**: 完善验证逻辑，单元测试覆盖

### 中风险
- **并发竞争**: 多次重载可能导致状态不一致
  - **缓解措施**: 使用读写锁保护共享状态

- **资源泄漏**: 旧连接未正确清理
  - **缓解措施**: 连接超时自动清理，定期检查

### 低风险
- **用户体验**: 重载期间可能有短暂延迟
  - **缓解措施**: 优化重载性能，异步处理

---

## 时间估算

| 步骤 | 预计时间 | 优先级 |
|------|---------|--------|
| 1. SIGHUP 信号处理器 | 2 小时 | 高 |
| 2. 配置重载核心逻辑 | 4 小时 | 高 |
| 3. 优雅连接处理 | 6 小时 | 高 |
| 4. 错误处理和回滚 | 3 小时 | 中 |
| 5. Web UI 界面增强 | 4 小时 | 中 |
| 6. 日志和监控 | 2 小时 | 中 |
| 7. 测试验收 | 6 小时 | 高 |
| **总计** | **27 小时** | - |

---

## 参考资料

- [Go 信号处理](https://pkg.go.dev/os/signal)
- [优雅重启最佳实践](https://grisha.org/blog/2014/06/03/graceful-restart-in-golang/)
- [配置热重载模式](https://martinfowler.com/bliki/ConfigurationHotReload.html)