package config

import (
	"testing"
)

func TestBaseConfigStrategy(t *testing.T) {
	strategy := &BaseConfigStrategy{}
	ctx := &ConfigContext{}

	config, err := strategy.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if config == "" {
		t.Fatal("Generated config is empty")
	}

	if !strategy.Validate(ctx) {
		t.Fatal("Validate failed")
	}
}

func TestWildcardCertStrategy(t *testing.T) {
	strategy := &WildcardCertStrategy{}

	t.Run("Valid context", func(t *testing.T) {
		ctx := &ConfigContext{
			Domain:          "example.com",
			CloudflareToken: "test-token",
		}

		if !strategy.Validate(ctx) {
			t.Fatal("Validate failed for valid context")
		}

		config, err := strategy.Generate(ctx)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		if config == "" {
			t.Fatal("Generated config is empty")
		}

		// 检查配置中是否包含域名
		if !contains(config, "example.com") {
			t.Error("Config does not contain domain")
		}

		// 检查配置中是否包含通配符
		if !contains(config, "*.example.com") {
			t.Error("Config does not contain wildcard domain")
		}
	})

	t.Run("Invalid context - missing domain", func(t *testing.T) {
		ctx := &ConfigContext{
			CloudflareToken: "test-token",
		}

		if strategy.Validate(ctx) {
			t.Fatal("Validate should fail for missing domain")
		}
	})

	t.Run("Invalid context - missing token", func(t *testing.T) {
		ctx := &ConfigContext{
			Domain: "example.com",
		}

		if strategy.Validate(ctx) {
			t.Fatal("Validate should fail for missing token")
		}
	})
}

func TestHTTPProxyStrategy(t *testing.T) {
	strategy := &HTTPProxyStrategy{}
	ctx := &ConfigContext{
		Domain:          "example.com",
		CloudflareToken: "token",
	}

	t.Run("Valid config", func(t *testing.T) {
		config, err := strategy.Generate(ctx, "web", 8080)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		if config == "" {
			t.Fatal("Generated config is empty")
		}

		// 检查域名
		if !contains(config, "web.example.com") {
			t.Error("Config does not contain full domain")
		}

		// 检查 http handler
		if !contains(config, "http:") {
			t.Error("Config does not contain http handler")
		}

		// 检查后端端口
		if !contains(config, "127.0.0.1:8080") {
			t.Error("Config does not contain backend port")
		}
	})

	t.Run("Missing subdomain", func(t *testing.T) {
		_, err := strategy.Generate(ctx, "", 8080)
		if err == nil {
			t.Fatal("Generate should fail for missing subdomain")
		}
	})
}

func TestWebSocketStrategy(t *testing.T) {
	strategy := &WebSocketStrategy{}
	ctx := &ConfigContext{
		Domain:          "example.com",
		CloudflareToken: "token",
	}

	config, err := strategy.Generate(ctx, "ws", 8443)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if config == "" {
		t.Fatal("Generated config is empty")
	}

	// 检查域名
	if !contains(config, "ws.example.com") {
		t.Error("Config does not contain full domain")
	}

	// 检查 default handler
	if !contains(config, "default:") {
		t.Error("Config does not contain default handler")
	}

	// 检查后端端口
	if !contains(config, "127.0.0.1:8443") {
		t.Error("Config does not contain backend port")
	}
}

func TestTCPProxyStrategy(t *testing.T) {
	strategy := &TCPProxyStrategy{}
	ctx := &ConfigContext{
		Domain:          "example.com",
		CloudflareToken: "token",
	}

	config, err := strategy.Generate(ctx, "api", 8443)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if config == "" {
		t.Fatal("Generated config is empty")
	}

	// 检查域名
	if !contains(config, "api.example.com") {
		t.Error("Config does not contain full domain")
	}

	// 检查 default handler
	if !contains(config, "default:") {
		t.Error("Config does not contain default handler")
	}

	// 检查后端端口
	if !contains(config, "127.0.0.1:8443") {
		t.Error("Config does not contain backend port")
	}
}

func TestConfigRequestBuilder(t *testing.T) {
	builder := NewConfigRequestBuilder()

	request := builder.
		SetDomain("test.com").
		SetCloudflareToken("token").
		AddHTTPService("web", 8080).
		AddTCPService("api", 8443).
		AddWebSocketService("ws", 9090).
		Build()

	if request.Domain != "test.com" {
		t.Errorf("Domain mismatch: got %s, want test.com", request.Domain)
	}

	if request.CloudflareToken != "token" {
		t.Errorf("Token mismatch")
	}

	if len(request.Services) != 3 {
		t.Errorf("Services count mismatch: got %d, want 3", len(request.Services))
	}

	// 检查服务类型
	if request.Services[0].Type != "http" {
		t.Errorf("First service type mismatch")
	}

	if request.Services[1].Type != "tcp" {
		t.Errorf("Second service type mismatch")
	}

	if request.Services[2].Type != "websocket" {
		t.Errorf("Third service type mismatch")
	}
}

func TestGenerateFullConfig(t *testing.T) {
	t.Run("Full config with all services", func(t *testing.T) {
		request := NewConfigRequestBuilder().
			SetDomain("example.com").
			SetCloudflareToken("test-token").
			AddHTTPService("web", 8080).
			AddTCPService("api", 8443).
			AddWebSocketService("ws", 8443).
			Build()

		config, err := GenerateFullConfig(request)
		if err != nil {
			t.Fatalf("GenerateFullConfig failed: %v", err)
		}

		if config == "" {
			t.Fatal("Generated config is empty")
		}

		// 检查基础配置
		if !contains(config, "listen: 0.0.0.0:443") {
			t.Error("Config missing listen address")
		}

		if !contains(config, "webui_listen: 127.0.0.1:8080") {
			t.Error("Config missing webui_listen")
		}

		// 检查通配符证书配置
		if !contains(config, "wildcard_certs:") {
			t.Error("Config missing wildcard_certs section")
		}

		if !contains(config, "*.example.com") {
			t.Error("Config missing wildcard domain")
		}

		// 检查 vhosts 配置
		if !contains(config, "vhosts:") {
			t.Error("Config missing vhosts section")
		}

		// 检查各个服务
		if !contains(config, "web.example.com") {
			t.Error("Config missing web service")
		}

		if !contains(config, "api.example.com") {
			t.Error("Config missing api service")
		}

		if !contains(config, "ws.example.com") {
			t.Error("Config missing ws service")
		}
	})

	t.Run("Config with only HTTP service", func(t *testing.T) {
		request := NewConfigRequestBuilder().
			SetDomain("simple.com").
			SetCloudflareToken("token").
			AddHTTPService("www", 8080).
			Build()

		config, err := GenerateFullConfig(request)
		if err != nil {
			t.Fatalf("GenerateFullConfig failed: %v", err)
		}

		if !contains(config, "www.simple.com") {
			t.Error("Config missing www service")
		}

		if !contains(config, "http:") {
			t.Error("Config missing http handler for www service")
		}
	})

	t.Run("Config with multiple HTTP services", func(t *testing.T) {
		request := NewConfigRequestBuilder().
			SetDomain("multi.com").
			SetCloudflareToken("token").
			AddHTTPService("blog", 3000).
			AddHTTPService("shop", 3001).
			AddHTTPService("admin", 3002).
			Build()

		config, err := GenerateFullConfig(request)
		if err != nil {
			t.Fatalf("GenerateFullConfig failed: %v", err)
		}

		if !contains(config, "blog.multi.com") {
			t.Error("Config missing blog service")
		}

		if !contains(config, "shop.multi.com") {
			t.Error("Config missing shop service")
		}

		if !contains(config, "admin.multi.com") {
			t.Error("Config missing admin service")
		}

		if !contains(config, "127.0.0.1:3000") {
			t.Error("Config missing blog backend port")
		}

		if !contains(config, "127.0.0.1:3001") {
			t.Error("Config missing shop backend port")
		}

		if !contains(config, "127.0.0.1:3002") {
			t.Error("Config missing admin backend port")
		}
	})
}

func TestConfigGenerator(t *testing.T) {
	t.Run("Add and clear strategies", func(t *testing.T) {
		generator := NewConfigGenerator()
		ctx := &ConfigContext{
			Domain:          "test.com",
			CloudflareToken: "token",
		}

		// 添加策略
		generator.AddStrategy(&BaseConfigStrategy{})
		generator.AddStrategy(&WildcardCertStrategy{})

		if len(generator.strategies) != 2 {
			t.Errorf("Strategy count mismatch: got %d, want 2", len(generator.strategies))
		}

		// 生成配置
		config, err := generator.Generate(ctx)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		if config == "" {
			t.Fatal("Generated config is empty")
		}

		// 清空策略
		generator.ClearStrategies()

		if len(generator.strategies) != 0 {
			t.Errorf("Strategy count mismatch after clear: got %d, want 0", len(generator.strategies))
		}
	})
}

// 辅助函数：检查字符串是否包含子字符串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}