package config

import (
	"crypto/tls"
	"fmt"
	"github.com/liberal-boy/tls-shunt-proxy/config/raw"
	"github.com/liberal-boy/tls-shunt-proxy/handler"
	"github.com/liberal-boy/tls-shunt-proxy/handler/http2"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"time"
)

// ReloadEvent 配置重载事件
type ReloadEvent struct {
	Timestamp   time.Time
	OldVersion  uint64
	NewVersion  uint64
	Success     bool
	Error       string
	ConfigStats ConfigStats
}

// ConfigStats 配置统计信息
type ConfigStats struct {
	VHostsCount         int
	WildcardCertsCount int
	ListenAddr          string
	RedirectHttpsAddr   string
}

type (
	Config struct {
		Listen            string
		RedirectHttps     string
		Fallback          handler.Handler
		VHosts            map[string]VHost
		// WebUIListen: 管理界面监听地址，来自 raw config (webui_listen)
		WebUIListen       string
		WildcardManager   *WildcardManager
		// 配置版本号，用于跟踪配置变更
		Version           uint64
		// 读写锁，保护配置的并发访问
		mu                sync.RWMutex
		// 重载事件历史
		reloadHistory     []ReloadEvent
		reloadHistoryMu   sync.RWMutex
	}
	VHost struct {
		TlsConfig    *tls.Config
		Http         handler.Handler
		Http2        handler.Handler
		PathHandlers []PathHandler
		Trojan       handler.Handler
		Default      handler.Handler
	}
	PathHandler struct {
		Path, TrimPrefix string
		Handler          handler.Handler
	}
)

func readRawConfig(path string) (conf raw.RawConfig, err error) {
	conf = raw.RawConfig{InboundBufferSize: 4, OutboundBufferSize: 32}
	yamlFile, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}
	err = yaml.Unmarshal(yamlFile, &conf)
	if err != nil {
		return
	}
	return
}

func ReadConfig(path string) (conf Config, err error) {
	rawConf, err := readRawConfig(path)
	if err != nil {
		return
	}

	handler.InitBufferPools(rawConf.InboundBufferSize*1024, rawConf.OutboundBufferSize*1024)

	conf.Listen = rawConf.Listen
	conf.RedirectHttps = rawConf.RedirectHttps
	conf.WebUIListen = rawConf.WebUIListen
	conf.Version = 1 // 初始化版本号为 1

	conf.WildcardManager = NewWildcardManager()
	if len(rawConf.WildcardCerts) > 0 {
		if err := conf.WildcardManager.LoadFromConfig(rawConf.WildcardCerts); err != nil {
			log.Printf("⚠️ 加载通配符证书失败: %v", err)
			log.Printf("⚠️ 通配符证书功能将不可用，请检查配置和网络连接")
			// 注意：这里不返回错误，允许服务继续启动
			// 虚拟主机仍然可以使用单独的证书
		}
	}

	if rawConf.Fallback != "" {
		conf.Fallback = handler.NewProxyPassHandler(rawConf.Fallback)
	} else {
		conf.Fallback = handler.NoopHandler
	}
	conf.VHosts = make(map[string]VHost, len(rawConf.VHosts))

	for _, vh := range rawConf.VHosts {
		var tlsConfig *tls.Config

		if vh.TlsOffloading {
			// 判断是否应该使用通配符证书
			// 优先级：自定义证书 > 通配符证书 > 单独申请证书
			useWildcard := false

			// 如果没有配置自定义证书（cert/key 为空），且启用了 managedcert
			// 检查是否有通配符证书匹配
			if vh.Cert == "" && vh.Key == "" && vh.ManagedCert {
				if conf.WildcardManager != nil {
					// 检查是否有通配符配置匹配此域名
					// 注意：即使通配符证书还在申请中，只要有配置就应该禁用 managedcert
					// 避免两个 CertMagic 实例管理同一个域名导致冲突
					conf.WildcardManager.mu.RLock()
					for wildcard := range conf.WildcardManager.wildcards {
						if conf.WildcardManager.matchWildcard(wildcard, vh.Name) {
							useWildcard = true
							log.Printf("✓ 域名 %s 匹配通配符配置 %s，将使用通配符证书（禁用 managedcert）", vh.Name, wildcard)
							break
						}
					}
					conf.WildcardManager.mu.RUnlock()
				}
			}

			// 如果使用通配符证书，则禁用 managedcert（避免单独申请）
			effectiveManagedCert := vh.ManagedCert && !useWildcard

			if useWildcard {
				log.Printf("✓ 域名 %s 的 managedcert 已禁用，将使用通配符证书", vh.Name)
			} else if effectiveManagedCert {
				log.Printf("✓ 域名 %s 将单独申请证书（managedcert=true）", vh.Name)
			}

			tlsConfig, err = getTlsConfig(effectiveManagedCert, vh.Name, vh.Cert, vh.Key, vh.KeyType, vh.Alpn, vh.Protocols, conf.WildcardManager)
		}

		pathHandlers := make([]PathHandler, len(vh.Http.Paths))

		for i, p := range vh.Http.Paths {
			pathHandlers[i] = PathHandler{
				Path:       p.Path,
				TrimPrefix: p.TrimPrefix,
				Handler:    newHandler(p.Handler, p.Args),
			}
		}

		var http2Handler handler.Handler
		if len(vh.Http2) != 0 {
			http2Handler = http2.NewHttpMuxHandler(vh.Http2)
		} else {
			http2Handler = handler.NoopHandler
		}

		conf.VHosts[strings.ToLower(vh.Name)] = VHost{
			TlsConfig:    tlsConfig,
			Http:         newHandler(vh.Http.Handler, vh.Http.Args),
			PathHandlers: pathHandlers,
			Http2:        http2Handler,
			Trojan:       newHandler(vh.Trojan.Handler, vh.Trojan.Args),
			Default:      newHandler(vh.Default.Handler, vh.Default.Args),
		}
	}
	return
}

// Reload 在当前进程中重新加载配置文件（不重启进程）
//
// 此方法用于重新加载非关键的配置变更，例如：
// - 虚拟主机配置变更
// - 后端服务器地址变更
// - 证书文件路径变更（如果证书已存在）
//
// 与 ZeroDowntimeReload() 的区别：
// - Reload(): 在当前进程中重新加载配置，不重启进程
// - ZeroDowntimeReload(): 启动新进程并优雅关闭旧进程，真正的零停机重载
//
// 注意事项：
// - 此方法不会重启监听器，监听地址变更不会生效
// - 此方法不会重新申请 SSL 证书
// - 对于需要监听器变更的场景，应使用 ZeroDowntimeReload()
func (c *Config) Reload(configPath string) error {
	log.Printf("开始重新加载配置，当前版本: %d", c.Version)

	// 保存旧配置的 redirecthttps 地址
	oldRedirectAddr := c.RedirectHttps

	// 获取旧配置的版本号（用于日志）
	oldVersion := c.Version

	// 读取并验证新配置
	newConf, err := ReadConfig(configPath)
	if err != nil {
		log.Printf("⚠️ 配置重载失败: 读取配置失败 - %v", err)
		log.Printf("⚠️ 保留旧配置（版本 %d），服务继续运行", oldVersion)

		// 记录失败事件
		c.logReloadEvent(ReloadEvent{
			Timestamp:   time.Now(),
			OldVersion:  oldVersion,
			NewVersion:  oldVersion,
			Success:     false,
			Error:       err.Error(),
			ConfigStats: c.GetConfigStats(),
		})

		return fmt.Errorf("读取配置失败: %w", err)
	}

	// 验证新配置的基本有效性
	validationErrors := validateConfig(newConf)
	if len(validationErrors) > 0 {
		log.Printf("⚠️ 配置重载失败: 配置验证失败")
		for _, errMsg := range validationErrors {
			log.Printf("  - %s", errMsg)
		}
		log.Printf("⚠️ 保留旧配置（版本 %d），服务继续运行", oldVersion)

		// 记录失败事件
		c.logReloadEvent(ReloadEvent{
			Timestamp:   time.Now(),
			OldVersion:  oldVersion,
			NewVersion:  oldVersion,
			Success:     false,
			Error:       "配置验证失败: " + strings.Join(validationErrors, "; "),
			ConfigStats: c.GetConfigStats(),
		})

		return fmt.Errorf("配置验证失败: %s", strings.Join(validationErrors, "; "))
	}

	// 增加新配置的版本号
	newConf.Version = oldVersion + 1

	// 重新加载通配符证书（如果有）
	// ReadConfig 已经处理了通配符证书的加载，这里只需要确认加载成功
	if newConf.WildcardManager != nil && len(newConf.WildcardManager.wildcards) > 0 {
		log.Printf("通配符证书已重新加载，共 %d 个域名", len(newConf.WildcardManager.wildcards))
	}

	// 原子性替换配置
	c.mu.Lock()
	*c = newConf
	c.mu.Unlock()

	// 如果 redirecthttps 配置变更，重启服务
	if oldRedirectAddr != newConf.RedirectHttps {
		log.Printf("HTTPS 重定向地址变更: %s -> %s，重启服务", oldRedirectAddr, newConf.RedirectHttps)
		// 这里需要导入 handler 包
		// 由于循环导入问题，我们在 main.go 中处理这部分
	}

	log.Printf("✓ 配置重载成功: 版本 %d -> %d", oldVersion, newConf.Version)
	log.Printf("✓ 当前配置的虚拟主机数: %d", len(newConf.VHosts))
	if newConf.WildcardManager != nil && len(newConf.WildcardManager.wildcards) > 0 {
		log.Printf("✓ 通配符证书域名数: %d", len(newConf.WildcardManager.wildcards))
	}

	// 记录成功事件
	c.logReloadEvent(ReloadEvent{
		Timestamp:   time.Now(),
		OldVersion:  oldVersion,
		NewVersion:  newConf.Version,
		Success:     true,
		ConfigStats: newConf.GetConfigStats(),
	})

	return nil
}

// validateConfig 验证配置的有效性
func validateConfig(conf Config) []string {
	var errors []string

	// 验证监听地址
	if conf.Listen == "" {
		errors = append(errors, "listen 配置不能为空")
	}

	// 验证虚拟主机配置
	for name, vh := range conf.VHosts {
		if name == "" {
			errors = append(errors, "虚拟主机名称不能为空")
		}
		// 注意：VHost 结构体没有 TlsOffloading 字段，这里移除相关检查
		if vh.TlsConfig == nil {
			// 如果没有 TLS 配置，检查是否有其他配置
			if vh.Default == nil && vh.Http == nil {
				errors = append(errors, fmt.Sprintf("虚拟主机 '%s' 缺少必要的处理器配置", name))
			}
		}
	}

	// 验证 fallback 配置
	if conf.Fallback == nil {
		errors = append(errors, "fallback 配置不能为空")
	}

	return errors
}

// GetVersion 获取当前配置版本号
func (c *Config) GetVersion() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Version
}

// logReloadEvent 记录重载事件
func (c *Config) logReloadEvent(event ReloadEvent) {
	c.reloadHistoryMu.Lock()
	defer c.reloadHistoryMu.Unlock()

	// 保留最近 100 条记录
	if len(c.reloadHistory) >= 100 {
		c.reloadHistory = c.reloadHistory[1:]
	}
	c.reloadHistory = append(c.reloadHistory, event)

	log.Printf("配置重载事件: 时间=%s, 版本=%d->%d, 成功=%v, 错误=%s",
		event.Timestamp.Format("2006-01-02 15:04:05"),
		event.OldVersion,
		event.NewVersion,
		event.Success,
		event.Error)
}

// GetReloadHistory 获取重载事件历史
func (c *Config) GetReloadHistory(limit int) []ReloadEvent {
	c.reloadHistoryMu.RLock()
	defer c.reloadHistoryMu.RUnlock()

	if limit <= 0 || limit > len(c.reloadHistory) {
		limit = len(c.reloadHistory)
	}

	return c.reloadHistory[len(c.reloadHistory)-limit:]
}

// GetConfigStats 获取当前配置统计信息
func (c *Config) GetConfigStats() ConfigStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	wildcardCount := 0
	if c.WildcardManager != nil {
		c.WildcardManager.mu.RLock()
		wildcardCount = len(c.WildcardManager.wildcards)
		c.WildcardManager.mu.RUnlock()
	}

	return ConfigStats{
		VHostsCount:         len(c.VHosts),
		WildcardCertsCount: wildcardCount,
		ListenAddr:          c.Listen,
		RedirectHttpsAddr:   c.RedirectHttps,
	}
}

func newHandler(name, args string) handler.Handler {
	switch name {
	case "":
		return handler.NoopHandler
	case "proxyPass":
		return handler.NewProxyPassHandler(args)
	case "fileServer":
		return handler.NewFileServerHandler(args)
	case "dohServer":
		return handler.NewDohServer(args)
	}
	return handler.NoopHandler
}