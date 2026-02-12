package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ConfigContext 配置上下文，包含生成配置所需的所有信息
type ConfigContext struct {
	Domain          string // 主域名
	CloudflareToken string // Cloudflare API Token
}

// ConfigStrategy 配置策略接口
type ConfigStrategy interface {
	// Generate 生成配置内容
	Generate(ctx *ConfigContext) (string, error)
	// Validate 验证配置上下文是否有效
	Validate(ctx *ConfigContext) bool
}

// ServiceConfig 服务配置
type ServiceConfig struct {
	Subdomain   string `json:"subdomain"`   // 子域名
	Type        string `json:"type"`        // 服务类型: http, websocket, tcp
	BackendPort int    `json:"backend_port"` // 后端端口
}

// ConfigRequest 配置请求
type ConfigRequest struct {
	Domain          string          `json:"domain"`           // 主域名
	CloudflareToken string          `json:"cloudflare_token"` // Cloudflare API Token
	Services        []ServiceConfig `json:"services"`         // 服务列表
}

// ToContext 转换为配置上下文
func (r *ConfigRequest) ToContext() *ConfigContext {
	return &ConfigContext{
		Domain:          r.Domain,
		CloudflareToken: r.CloudflareToken,
	}
}

// BaseConfigStrategy 基础配置策略
type BaseConfigStrategy struct{}

func (s *BaseConfigStrategy) Generate(ctx *ConfigContext) (string, error) {
	return `listen: 0.0.0.0:443
redirecthttps: 0.0.0.0:80
inboundbuffersize: 4
outboundbuffersize: 32
fallback: 127.0.0.1:8443
webui_listen: 127.0.0.1:8080
`, nil
}

func (s *BaseConfigStrategy) Validate(ctx *ConfigContext) bool {
	return true
}

// WildcardCertStrategy 通配符证书配置策略
type WildcardCertStrategy struct{}

func (s *WildcardCertStrategy) Generate(ctx *ConfigContext) (string, error) {
	if ctx.Domain == "" {
		return "", fmt.Errorf("domain is required")
	}

	// 验证 Cloudflare Token 格式（基本验证）
	if len(ctx.CloudflareToken) == 0 || len(ctx.CloudflareToken) > 255 {
		return "", fmt.Errorf("invalid cloudflare token length")
	}

	// 验证域名格式
	if !isValidDomain(ctx.Domain) {
		return "", fmt.Errorf("invalid domain format")
	}

	credentials, err := json.Marshal(map[string]string{
		"api_token": ctx.CloudflareToken,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal credentials: %w", err)
	}

	return fmt.Sprintf(`# cloudflare认证成功，生成下面块
wildcard_certs:
  - domain: "*.%s"
    dns_provider: "cloudflare"
    dns_credentials: '%s'
    ca_provider: "letsencrypt"
    enable_backup_ca: false
`, ctx.Domain, string(credentials)), nil
}

// isValidDomain 验证域名格式
func isValidDomain(domain string) bool {
	if domain == "" || len(domain) > 253 {
		return false
	}

	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false
	}

	for _, part := range parts {
		if part == "" || len(part) > 63 {
			return false
		}

		// 检查每个部分是否只包含字母数字和连字符
		for i, char := range part {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') || char == '-') {
				return false
			}

			// 连字符不能在开头或结尾
			if (char == '-' && (i == 0 || i == len(part)-1)) {
				return false
			}
		}
	}

	return true
}

func (s *WildcardCertStrategy) Validate(ctx *ConfigContext) bool {
	return ctx.Domain != "" && ctx.CloudflareToken != ""
}

// BaseVHostStrategy 虚拟主机配置基类
type BaseVHostStrategy struct{}

func (s *BaseVHostStrategy) generateCommonVHostConfig(name string) string {
	return fmt.Sprintf(`  - name: %s
    tlsoffloading: true
    managedcert: true
    keytype: p256
    alpn: http/1.1,h2
    protocols: tls12,tls13`, name)
}

// HTTPProxyStrategy HTTP代理配置策略
type HTTPProxyStrategy struct {
	BaseVHostStrategy
}

func (s *HTTPProxyStrategy) Generate(ctx *ConfigContext, subdomain string, backendPort int) (string, error) {
	if subdomain == "" {
		return "", fmt.Errorf("subdomain is required")
	}

	fullDomain := fmt.Sprintf("%s.%s", subdomain, ctx.Domain)
	config := s.generateCommonVHostConfig(fullDomain)
	config += fmt.Sprintf(`
    http:
      handler: proxyPass
      args: 127.0.0.1:%d`, backendPort)

	return config, nil
}

func (s *HTTPProxyStrategy) Validate(ctx *ConfigContext) bool {
	return ctx.Domain != ""
}

// WebSocketStrategy WebSocket代理配置策略
type WebSocketStrategy struct {
	BaseVHostStrategy
}

func (s *WebSocketStrategy) Generate(ctx *ConfigContext, subdomain string, backendPort int) (string, error) {
	if subdomain == "" {
		return "", fmt.Errorf("subdomain is required")
	}

	fullDomain := fmt.Sprintf("%s.%s", subdomain, ctx.Domain)
	config := s.generateCommonVHostConfig(fullDomain)
	config += fmt.Sprintf(`
    default:
      handler: proxyPass
      args: 127.0.0.1:%d`, backendPort)

	return config, nil
}

func (s *WebSocketStrategy) Validate(ctx *ConfigContext) bool {
	return ctx.Domain != ""
}

// TCPProxyStrategy TCP代理配置策略
type TCPProxyStrategy struct {
	BaseVHostStrategy
}

func (s *TCPProxyStrategy) Generate(ctx *ConfigContext, subdomain string, backendPort int) (string, error) {
	if subdomain == "" {
		return "", fmt.Errorf("subdomain is required")
	}

	fullDomain := fmt.Sprintf("%s.%s", subdomain, ctx.Domain)
	config := s.generateCommonVHostConfig(fullDomain)
	config += fmt.Sprintf(`
    default:
      handler: proxyPass
      args: 127.0.0.1:%d`, backendPort)

	return config, nil
}

func (s *TCPProxyStrategy) Validate(ctx *ConfigContext) bool {
	return ctx.Domain != ""
}

// ConfigGenerator 配置生成器
type ConfigGenerator struct {
	strategies []ConfigStrategy
}

// NewConfigGenerator 创建配置生成器
func NewConfigGenerator() *ConfigGenerator {
	return &ConfigGenerator{
		strategies: make([]ConfigStrategy, 0),
	}
}

// AddStrategy 添加配置策略
func (g *ConfigGenerator) AddStrategy(strategy ConfigStrategy) *ConfigGenerator {
	g.strategies = append(g.strategies, strategy)
	return g
}

// Generate 生成配置
func (g *ConfigGenerator) Generate(ctx *ConfigContext) (string, error) {
	var configParts []string

	for _, strategy := range g.strategies {
		if strategy.Validate(ctx) {
			config, err := strategy.Generate(ctx)
			if err != nil {
				return "", fmt.Errorf("strategy generate failed: %w", err)
			}
			if config != "" {
				configParts = append(configParts, config)
			}
		}
	}

	return strings.Join(configParts, "\n"), nil
}

// ClearStrategies 清空所有策略
func (g *ConfigGenerator) ClearStrategies() *ConfigGenerator {
	g.strategies = make([]ConfigStrategy, 0)
	return g
}

// GenerateFullConfig 生成完整配置
func GenerateFullConfig(request *ConfigRequest) (string, error) {
	generator := NewConfigGenerator()

	// 添加基础配置策略
	generator.AddStrategy(&BaseConfigStrategy{})

	// 添加通配符证书策略
	generator.AddStrategy(&WildcardCertStrategy{})

	// 生成基础配置
	ctx := request.ToContext()
	baseConfig, err := generator.Generate(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to generate base config: %w", err)
	}

	// 生成 vhosts 配置
	var vhostsParts []string
	vhostsParts = append(vhostsParts, "vhosts:")

	// 创建策略实例
	httpStrategy := &HTTPProxyStrategy{}
	websocketStrategy := &WebSocketStrategy{}
	tcpStrategy := &TCPProxyStrategy{}

	for _, service := range request.Services {
		var vhostConfig string
		var err error

		switch service.Type {
		case "http":
			vhostConfig, err = httpStrategy.Generate(ctx, service.Subdomain, service.BackendPort)
		case "websocket":
			vhostConfig, err = websocketStrategy.Generate(ctx, service.Subdomain, service.BackendPort)
		case "tcp":
			vhostConfig, err = tcpStrategy.Generate(ctx, service.Subdomain, service.BackendPort)
		default:
			continue
		}

		if err != nil {
			return "", fmt.Errorf("failed to generate vhost config for %s: %w", service.Subdomain, err)
		}

		if vhostConfig != "" {
			vhostsParts = append(vhostsParts, vhostConfig)
		}
	}

	// 组合所有配置，vhost 之间用空行分隔
	return baseConfig + "\n" + strings.Join(vhostsParts, "\n\n"), nil
}

// ConfigRequestBuilder 配置请求构建器
type ConfigRequestBuilder struct {
	domain          string
	cloudflareToken string
	services        []ServiceConfig
}

// NewConfigRequestBuilder 创建配置请求构建器
func NewConfigRequestBuilder() *ConfigRequestBuilder {
	return &ConfigRequestBuilder{
		services: make([]ServiceConfig, 0),
	}
}

// SetDomain 设置主域名
func (b *ConfigRequestBuilder) SetDomain(domain string) *ConfigRequestBuilder {
	b.domain = domain
	return b
}

// SetCloudflareToken 设置 Cloudflare Token
func (b *ConfigRequestBuilder) SetCloudflareToken(token string) *ConfigRequestBuilder {
	b.cloudflareToken = token
	return b
}

// AddHTTPService 添加HTTP服务
func (b *ConfigRequestBuilder) AddHTTPService(subdomain string, backendPort int) *ConfigRequestBuilder {
	b.services = append(b.services, ServiceConfig{
		Subdomain:   subdomain,
		Type:        "http",
		BackendPort: backendPort,
	})
	return b
}

// AddWebSocketService 添加WebSocket服务
func (b *ConfigRequestBuilder) AddWebSocketService(subdomain string, backendPort int) *ConfigRequestBuilder {
	b.services = append(b.services, ServiceConfig{
		Subdomain:   subdomain,
		Type:        "websocket",
		BackendPort: backendPort,
	})
	return b
}

// AddTCPService 添加TCP服务
func (b *ConfigRequestBuilder) AddTCPService(subdomain string, backendPort int) *ConfigRequestBuilder {
	b.services = append(b.services, ServiceConfig{
		Subdomain:   subdomain,
		Type:        "tcp",
		BackendPort: backendPort,
	})
	return b
}

// Build 构建配置请求
func (b *ConfigRequestBuilder) Build() *ConfigRequest {
	return &ConfigRequest{
		Domain:          b.domain,
		CloudflareToken: b.cloudflareToken,
		Services:        b.services,
	}
}