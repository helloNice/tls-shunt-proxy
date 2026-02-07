package config

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/caddyserver/certmagic"
	"github.com/liberal-boy/tls-shunt-proxy/config/raw"
	"github.com/libdns/cloudflare"
	"log"
	"strings"
	"sync"
	"time"
)

type WildcardManager struct {
	magic      *certmagic.Config          // 默认 certmagic 实例（向后兼容）
	magicMap   map[string]*certmagic.Config // 通配符域名 -> 独立的 certmagic 实例
	certMap    map[string]string         // 子域名 -> 通配符域名映射
	wildcards  map[string]raw.WildcardCertConfig // 通配符域名配置
	mu         sync.RWMutex
}

func NewWildcardManager() *WildcardManager {
	return &WildcardManager{
		certMap:   make(map[string]string),
		wildcards: make(map[string]raw.WildcardCertConfig),
		magicMap:  make(map[string]*certmagic.Config),
	}
}

// getDNSProvider 根据 DNS Provider 类型创建对应的 libdns Provider
func getDNSProvider(providerType, credentials string) (interface{}, error) {
	// 向后兼容：如果使用旧配置格式
	if providerType == "" && credentials == "" {
		return nil, fmt.Errorf("未指定 DNS Provider 类型或凭据")
	}

	// 旧配置格式：使用 cloudflare_key
	if providerType == "" && credentials == "" {
		// 这种情况由调用方处理
		return nil, fmt.Errorf("使用旧配置格式")
	}

	switch strings.ToLower(providerType) {
	case "cloudflare":
		// 新配置格式：使用 dns_credentials (JSON)
		if credentials != "" {
			var cfCreds struct {
				APIToken string `json:"api_token"`
			}
			if err := json.Unmarshal([]byte(credentials), &cfCreds); err != nil {
				return nil, fmt.Errorf("解析 Cloudflare 凭据失败: %w", err)
			}
			return &cloudflare.Provider{
				APIToken: cfCreds.APIToken,
			}, nil
		}
		return nil, fmt.Errorf("Cloudflare Provider 需要凭据")
	default:
		return nil, fmt.Errorf("不支持的 DNS Provider 类型: %s (当前仅支持: cloudflare)", providerType)
	}
}

func (w *WildcardManager) LoadFromConfig(configs []raw.WildcardCertConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(configs) == 0 {
		log.Println("未配置通配符证书")
		return nil
	}

	// 验证配置
	var validConfigs []raw.WildcardCertConfig
	for i, wc := range configs {
		// 验证域名格式
		if wc.Domain == "" {
			log.Printf("⚠️ 通配符证书配置 #%d: 域名为空，已跳过", i+1)
			continue
		}

		// 验证是否为通配符域名
		if !strings.HasPrefix(wc.Domain, "*.") {
			log.Printf("⚠️ 通配符证书配置 #%d: 域名 '%s' 不是通配符格式（应以 *. 开头），已跳过", i+1, wc.Domain)
			continue
		}

		// 验证域名后缀是否有效
		suffix := wc.Domain[2:]
		if suffix == "" || strings.Contains(suffix, "*") {
			log.Printf("⚠️ 通配符证书配置 #%d: 域名 '%s' 格式无效，已跳过", i+1, wc.Domain)
			continue
		}

		// 验证 API Key
		if wc.CloudflareKey == "" {
			log.Printf("⚠️ 通配符证书配置 #%d: 域名 '%s' 的 API Key 为空，已跳过", i+1, wc.Domain)
			continue
		}

		// 检查重复配置
		if _, exists := w.wildcards[wc.Domain]; exists {
			log.Printf("⚠️ 通配符证书配置 #%d: 域名 '%s' 已存在，跳过重复配置", i+1, wc.Domain)
			continue
		}

		// 配置有效，添加到列表
		w.wildcards[wc.Domain] = wc
		validConfigs = append(validConfigs, wc)
		log.Printf("✓ 通配符证书配置已验证: %s", wc.Domain)
	}

	if len(validConfigs) == 0 {
		return fmt.Errorf("没有有效的通配符证书配置")
	}

	// 为每个通配符域名创建独立的 certmagic 实例
	// 这样可以确保每个域名使用对应的 API Key，避免 DNS 验证失败
	for _, wc := range validConfigs {
		var cfProvider *cloudflare.Provider

		// 支持新旧两种配置格式
		if wc.DNSProvider != "" && wc.DNSCredentials != "" {
			// 新配置格式
			provider, err := getDNSProvider(wc.DNSProvider, wc.DNSCredentials)
			if err != nil {
				log.Printf("⚠️ 域名 '%s' 的 DNS Provider 配置错误: %v，已跳过", wc.Domain, err)
				continue
			}
			cfProvider = provider.(*cloudflare.Provider)
		} else if wc.CloudflareKey != "" {
			// 旧配置格式（向后兼容）
			cfProvider = &cloudflare.Provider{
				APIToken: wc.CloudflareKey,
			}
		} else {
			log.Printf("⚠️ 域名 '%s' 未配置 DNS Provider 凭据，已跳过", wc.Domain)
			continue
		}

		// 创建独立的 cache 和 certmagic 实例
		cache := certmagic.NewCache(certmagic.CacheOptions{})
		certConfig := certmagic.Config{
			Storage: &certmagic.FileStorage{Path: "./"},
		}

		// 为该域名创建专用的 Issuer
		acmeIssuer := certmagic.NewACMEIssuer(&certConfig, certmagic.ACMEIssuer{
			DNS01Solver: &certmagic.DNS01Solver{
				DNSManager: certmagic.DNSManager{
					DNSProvider: cfProvider,
				},
			},
		})
		certConfig.Issuers = []certmagic.Issuer{acmeIssuer}

		// 创建独立的 certmagic 实例
		magic := certmagic.New(cache, certConfig)
		w.magicMap[wc.Domain] = magic

		log.Printf("✓ 为通配符域名 '%s' 创建独立的 certmagic 实例", wc.Domain)
	}

	// 申请所有通配符证书
	var domains []string
	for _, wc := range validConfigs {
		domains = append(domains, wc.Domain)
	}

	log.Printf("开始申请通配符证书，共 %d 个域名", len(domains))

	// 使用带超时的 context，设置 10 分钟超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 为每个独立的 certmagic 实例申请证书
	var certErrors []string
	for _, wc := range validConfigs {
		magic, ok := w.magicMap[wc.Domain]
		if !ok {
			log.Printf("⚠️ 域名 '%s' 的 certmagic 实例未找到", wc.Domain)
			continue
		}

		err := magic.ManageAsync(ctx, []string{wc.Domain})
		if err != nil {
			log.Printf("⚠️ 申请通配符证书失败 '%s': %v", wc.Domain, err)
			certErrors = append(certErrors, fmt.Sprintf("%s: %v", wc.Domain, err))
		} else {
			log.Printf("✓ 通配符证书申请成功: %s", wc.Domain)
		}
	}

	if len(certErrors) > 0 {
		return fmt.Errorf("部分通配符证书申请失败: %v", certErrors)
	}

	log.Printf("所有通配符证书申请成功，共 %d 个域名: %v", len(domains), domains)
	return nil
}

func (w *WildcardManager) GetCertificate(domain string) *tls.Config {
	// 先用读锁检查缓存
	w.mu.RLock()
	if wildcard, ok := w.certMap[domain]; ok {
		w.mu.RUnlock()
		// 返回对应通配符域名的独立 certmagic 实例的 TLS 配置
		if magic, ok := w.magicMap[wildcard]; ok {
			return &tls.Config{
				GetCertificate: magic.GetCertificate,
			}
		}
		return nil
	}
	w.mu.RUnlock()

	// 需要写入时升级为写锁
	w.mu.Lock()
	defer w.mu.Unlock()

	// 双重检查，防止重复写入
	if wildcard, ok := w.certMap[domain]; ok {
		if magic, ok := w.magicMap[wildcard]; ok {
			return &tls.Config{
				GetCertificate: magic.GetCertificate,
			}
		}
		return nil
	}

	// 匹配通配符并写入缓存
	for wildcard := range w.wildcards {
		if w.matchWildcard(wildcard, domain) {
			w.certMap[domain] = wildcard
			// 返回对应通配符域名的独立 certmagic 实例的 TLS 配置
			if magic, ok := w.magicMap[wildcard]; ok {
				return &tls.Config{
					GetCertificate: magic.GetCertificate,
				}
			}
		}
	}
	return nil
}

func (w *WildcardManager) matchWildcard(wildcard, domain string) bool {
	if !strings.HasPrefix(wildcard, "*.") {
		return false
	}
	suffix := wildcard[2:]
	// 通配符 *.example.com 只匹配子域名（如 www.example.com），不匹配裸域名 example.com
	// 符合 RFC 2818 和通配符证书的标准行为
	return strings.HasSuffix(domain, "."+suffix)
}