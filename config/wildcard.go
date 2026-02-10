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

// CA Provider 常量
const (
	CAProviderLetsEncrypt = "letsencrypt"
	CAProviderZeroSSL     = "zerossl"
	CAProviderBuypass     = "buypass"
	CAProviderGoogle      = "google"
)

// getCAEndpoint 根据 CA Provider 类型返回 ACME 目录 URL
func getCAEndpoint(provider string) string {
	switch strings.ToLower(provider) {
	case CAProviderLetsEncrypt, "":
		return certmagic.LetsEncryptProductionCA
	case CAProviderZeroSSL:
		return certmagic.ZeroSSLProductionCA
	case CAProviderBuypass:
		return "https://acme.buypass.com/acme/directory"
	case CAProviderGoogle:
		return "https://dv.acme-v02.api.pki.goog/directory"
	default:
		log.Printf("⚠️ 不支持的 CA Provider: %s，使用默认的 Let's Encrypt", provider)
		return certmagic.LetsEncryptProductionCA
	}
}

// getBackupCAEndpoint 返回备用 CA endpoint
func getBackupCAEndpoint(backupCA string) string {
	if backupCA == "" {
		return certmagic.ZeroSSLProductionCA // 默认使用 ZeroSSL 作为备用
	}
	return getCAEndpoint(backupCA)
}

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
	// 向后兼容：如果使用旧配置格式，这种情况由调用方处理
	if providerType == "" && credentials == "" {
		return nil, fmt.Errorf("使用旧配置格式（应使用 cloudflare_key）")
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

// isValidCAProvider 验证 CA Provider 是否支持
func isValidCAProvider(provider string) bool {
	switch strings.ToLower(provider) {
	case "", CAProviderLetsEncrypt, CAProviderZeroSSL, CAProviderBuypass, CAProviderGoogle:
		return true
	default:
		return false
	}
}

// validateConfig 验证单个通配符证书配置
func (w *WildcardManager) validateConfig(wc raw.WildcardCertConfig, index int) error {
	// 验证域名格式
	if wc.Domain == "" {
		return fmt.Errorf("域名为空")
	}

	if !strings.HasPrefix(wc.Domain, "*.") {
		return fmt.Errorf("域名 '%s' 不是通配符格式（应以 *. 开头）", wc.Domain)
	}

	// 验证域名后缀是否有效
	suffix := wc.Domain[2:]
	if suffix == "" || strings.Contains(suffix, "*") {
		return fmt.Errorf("域名 '%s' 格式无效", wc.Domain)
	}

	// 验证 DNS Provider 凭据
	hasOldFormat := wc.CloudflareKey != ""
	hasNewFormat := wc.DNSProvider != "" && wc.DNSCredentials != ""

	if !hasOldFormat && !hasNewFormat {
		return fmt.Errorf("未配置 DNS Provider 凭据，请配置 cloudflare_key 或 dns_provider + dns_credentials")
	}

	// 验证 CA Provider
	if wc.CAProvider != "" && !isValidCAProvider(wc.CAProvider) {
		return fmt.Errorf("不支持的 CA Provider '%s'，支持的 CA: letsencrypt, zerossl, buypass, google", wc.CAProvider)
	}

	// 验证备用 CA
	if wc.BackupCA != "" && !isValidCAProvider(wc.BackupCA) {
		return fmt.Errorf("不支持的备用 CA '%s'，支持的 CA: letsencrypt, zerossl, buypass, google", wc.BackupCA)
	}

	// 验证重试配置
	if wc.MaxRetries < 0 {
		return fmt.Errorf("max_retries 不能为负数: %d", wc.MaxRetries)
	}
	if wc.RetryDelay < 0 {
		return fmt.Errorf("retry_delay 不能为负数: %d", wc.RetryDelay)
	}

	// 验证 ZeroSSL 邮箱配置
	// ZeroSSL 必须配置邮箱，否则证书申请会失败
	needsEmail := false

	// 检查主 CA 是否使用 ZeroSSL
	if wc.CAProvider == CAProviderZeroSSL {
		needsEmail = true
	}

	// 检查是否启用备用 CA 且备用 CA 为 ZeroSSL
	enableBackup := true // 默认启用
	if wc.EnableBackupCA != nil {
		enableBackup = *wc.EnableBackupCA
	}

	if enableBackup {
		backupCA := wc.BackupCA
		if backupCA == "" {
			backupCA = CAProviderZeroSSL // 默认使用 ZeroSSL 作为备用
		}
		if backupCA == CAProviderZeroSSL {
			needsEmail = true
		}
	}

	if needsEmail && wc.Email == "" {
		return fmt.Errorf("使用 ZeroSSL 但未配置 email，请添加 email 字段")
	}

	return nil
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
	var validationErrors []string

	for i, wc := range configs {
		// 调用独立的验证函数
		if err := w.validateConfig(wc, i); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("配置 #%d (%s): %v", i+1, wc.Domain, err))
			continue
		}

		// 检查重复配置
		if _, exists := w.wildcards[wc.Domain]; exists {
			validationErrors = append(validationErrors, fmt.Sprintf("配置 #%d: 域名 '%s' 已存在", i+1, wc.Domain))
			continue
		}

		// 配置有效，添加到列表
		w.wildcards[wc.Domain] = wc
		validConfigs = append(validConfigs, wc)
		log.Printf("✓ 通配符证书配置已验证: %s", wc.Domain)
	}

	// 如果有验证错误，先返回
	if len(validationErrors) > 0 {
		errMsg := fmt.Sprintf("通配符证书配置验证失败 (%d 个错误):\n", len(validationErrors))
		for _, err := range validationErrors {
			errMsg += fmt.Sprintf("  - %s\n", err)
		}
		return fmt.Errorf(errMsg)
	}

	if len(validConfigs) == 0 {
		return fmt.Errorf("没有有效的通配符证书配置")
	}

	// 为每个通配符域名创建独立的 certmagic 实例
	// 这样可以确保每个域名使用对应的 API Key，避免 DNS 验证失败
	var setupErrors []string
	for _, wc := range validConfigs {
		var cfProvider *cloudflare.Provider
		var err error

		// 支持新旧两种配置格式
		if wc.DNSProvider != "" && wc.DNSCredentials != "" {
			// 新配置格式
			var provider interface{}
			provider, err = getDNSProvider(wc.DNSProvider, wc.DNSCredentials)
			if err != nil {
				setupErrors = append(setupErrors, fmt.Sprintf("域名 '%s': DNS Provider 配置错误 - %v", wc.Domain, err))
				continue
			}
			// 安全的类型断言
			var ok bool
			cfProvider, ok = provider.(*cloudflare.Provider)
			if !ok {
				setupErrors = append(setupErrors, fmt.Sprintf("域名 '%s': DNS Provider 类型转换失败", wc.Domain))
				continue
			}
		} else if wc.CloudflareKey != "" {
			// 旧配置格式（向后兼容）
			cfProvider = &cloudflare.Provider{
				APIToken: wc.CloudflareKey,
			}
		} else {
			setupErrors = append(setupErrors, fmt.Sprintf("域名 '%s': 未配置 DNS Provider 凭据", wc.Domain))
			continue
		}

		// 创建独立的 certmagic 实例
		certConfig := certmagic.Config{
			Storage: &certmagic.FileStorage{Path: "./"},
		}

		// 创建 cache
		cache := certmagic.NewCache(certmagic.CacheOptions{
			GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
				return &certConfig, nil
			},
		})

		// 获取主 CA
		primaryCA := wc.CAProvider
		if primaryCA == "" {
			primaryCA = CAProviderLetsEncrypt // 默认使用 Let's Encrypt
		}

		// 获取 Email（配置验证阶段已检查 ZeroSSL 必须有邮箱）
		email := wc.Email

		// 创建主 CA Issuer
		primaryIssuer := certmagic.NewACMEIssuer(&certConfig, certmagic.ACMEIssuer{
			CA:    getCAEndpoint(primaryCA),
			Email: email,
			DNS01Solver: &certmagic.DNS01Solver{
				DNSManager: certmagic.DNSManager{
					DNSProvider: cfProvider,
				},
			},
		})

		// 决定是否启用备用 CA
		// nil 表示未配置，默认启用备用 CA
		// true 表示明确启用
		// false 表示明确禁用
		enableBackup := true // 默认启用
		if wc.EnableBackupCA != nil {
			enableBackup = *wc.EnableBackupCA
		}

		var issuers []certmagic.Issuer
		issuers = append(issuers, primaryIssuer)

		// 配置备用 CA
		if enableBackup {
			backupCAName := wc.BackupCA
			if backupCAName == "" {
				backupCAName = CAProviderZeroSSL // 默认使用 ZeroSSL 作为备用
			}

			// 避免主备 CA 相同
			if strings.ToLower(backupCAName) != strings.ToLower(primaryCA) {
				backupIssuer := certmagic.NewACMEIssuer(&certConfig, certmagic.ACMEIssuer{
					CA:    getBackupCAEndpoint(backupCAName),
					Email: email,
					DNS01Solver: &certmagic.DNS01Solver{
						DNSManager: certmagic.DNSManager{
							DNSProvider: cfProvider,
						},
					},
				})
				issuers = append(issuers, backupIssuer)
				log.Printf("✓ 为通配符域名 '%s' 配置主 CA: %s, 备用 CA: %s",
					wc.Domain, primaryCA, backupCAName)
			} else {
				log.Printf("✓ 为通配符域名 '%s' 配置 CA: %s (主备相同，仅使用一个)",
					wc.Domain, primaryCA)
			}
		} else {
			log.Printf("✓ 为通配符域名 '%s' 配置 CA: %s (未启用备用 CA)",
				wc.Domain, primaryCA)
		}

		certConfig.Issuers = issuers

		// 创建独立的 certmagic 实例
		magic := certmagic.New(cache, certConfig)
		w.magicMap[wc.Domain] = magic

		log.Printf("✓ 为通配符域名 '%s' 创建独立的 certmagic 实例", wc.Domain)
	}

	// 检查是否有配置错误
	if len(setupErrors) > 0 {
		errMsg := fmt.Sprintf("通配符证书配置设置失败 (%d 个错误):\n", len(setupErrors))
		for _, err := range setupErrors {
			errMsg += fmt.Sprintf("  - %s\n", err)
		}
		return fmt.Errorf(errMsg)
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

	// 使用并发申请证书以提高效率
	var wg sync.WaitGroup
	var certErrors []string
	var certErrorsMutex sync.Mutex

	for _, wc := range validConfigs {
		wg.Add(1)
		go func(config raw.WildcardCertConfig) {
			defer wg.Done()

			magic, ok := w.magicMap[config.Domain]
			if !ok {
				log.Printf("⚠️ 域名 '%s' 的 certmagic 实例未找到", config.Domain)
				certErrorsMutex.Lock()
				certErrors = append(certErrors, fmt.Sprintf("%s: certmagic 实例未找到", config.Domain))
				certErrorsMutex.Unlock()
				return
			}

			// 配置重试参数
			maxRetries := config.MaxRetries
			if maxRetries == 0 {
				maxRetries = 3 // 默认重试 3 次
			}

			retryDelay := config.RetryDelay
			if retryDelay == 0 {
				retryDelay = 30 // 默认重试间隔 30 秒
			}

			// 带重试的证书申请
			var err error
			for attempt := 0; attempt < maxRetries; attempt++ {
				// 使用同步的 ManageSync 方法等待证书申请完成
				err = magic.ManageSync(ctx, []string{config.Domain})
				if err == nil {
					log.Printf("✓ 通配符证书申请成功: %s", config.Domain)
					break // 成功，跳出循环
				}

				// 失败处理
				if attempt < maxRetries-1 {
					log.Printf("⚠️ 申请证书失败 '%s' (尝试 %d/%d): %v，%d 秒后重试",
						config.Domain, attempt+1, maxRetries, err, retryDelay)
					time.Sleep(time.Duration(retryDelay) * time.Second)
				} else {
					log.Printf("⚠️ 申请通配符证书失败 '%s' (重试 %d 次后仍然失败): %v", config.Domain, maxRetries, err)
				}
			}

			// 记录最终结果
			if err != nil {
				certErrorsMutex.Lock()
				certErrors = append(certErrors, fmt.Sprintf("%s: %v", config.Domain, err))
				certErrorsMutex.Unlock()
			}
		}(wc)
	}

	// 等待所有证书申请完成
	wg.Wait()

	if len(certErrors) > 0 {
		errMsg := fmt.Sprintf("部分通配符证书申请失败 (%d/%d):\n", len(certErrors), len(validConfigs))
		for _, err := range certErrors {
			errMsg += fmt.Sprintf("  - %s\n", err)
		}
		return fmt.Errorf(errMsg)
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