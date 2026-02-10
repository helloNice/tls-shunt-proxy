package config

import (
	"fmt"
	"log"
)

// ExampleUsage 演示配置生成器的使用方法
func ExampleUsage() {
	fmt.Println("=== 示例1: 使用构建器模式生成完整配置 ===")

	// 使用构建器模式创建配置请求
	request := NewConfigRequestBuilder().
		SetDomain("hynjhaiwk.com").
		SetCloudflareToken("your-cloudflare-api-token").
		AddHTTPService("web", 8080).
		AddTCPService("api", 8443).
		AddWebSocketService("ws", 8443).
		Build()

	// 生成配置
	config, err := GenerateFullConfig(request)
	if err != nil {
		log.Fatalf("生成配置失败: %v", err)
	}

	fmt.Println(config)
	fmt.Print("\n")
}

// ExampleFromWebData 演示从Web界面数据生成配置
func ExampleFromWebData() {
	fmt.Println("=== 示例2: 从Web界面提交的数据生成配置 ===")

	// 模拟从Web界面接收的数据
	webData := &ConfigRequest{
		Domain:          "mysite.com",
		CloudflareToken: "abc123xyz",
		Services: []ServiceConfig{
			{Subdomain: "web", Type: "http", BackendPort: 8080},
			{Subdomain: "api", Type: "tcp", BackendPort: 9000},
			{Subdomain: "ws", Type: "websocket", BackendPort: 9090},
		},
	}

	// 生成配置
	config, err := GenerateFullConfig(webData)
	if err != nil {
		log.Fatalf("生成配置失败: %v", err)
	}

	fmt.Println(config)
	fmt.Print("\n")
}

// ExampleCustomHTTPStrategy 演示自定义HTTP策略
func ExampleCustomHTTPStrategy() {
	fmt.Println("=== 示例3: 自定义HTTP策略 ===")
	fmt.Print("说明: 只修改HTTPProxyStrategy，不影响其他策略\n\n")

	// 创建自定义的HTTP策略（模拟后期调整）
	type CustomHTTPProxyStrategy struct {
		HTTPProxyStrategy
	}

	customStrategy := &CustomHTTPProxyStrategy{}

	// 使用自定义策略生成HTTP虚拟主机配置
	ctx := &ConfigContext{
		Domain:          "example.com",
		CloudflareToken: "token",
	}

	config, err := customStrategy.Generate(ctx, "web", 8080)
	if err != nil {
		log.Fatalf("生成配置失败: %v", err)
	}

	fmt.Println(config)
	fmt.Print("注意: HTTP策略已修改，但WebSocket和TCP策略保持不变\n\n")
}

// ExampleMultipleHTTPServices 演示多个HTTP服务
func ExampleMultipleHTTPServices() {
	fmt.Println("=== 示例4: 多个HTTP服务 ===")

	request := NewConfigRequestBuilder().
		SetDomain("example.com").
		SetCloudflareToken("custom-token").
		AddHTTPService("blog", 3000).
		AddHTTPService("shop", 3001).
		AddHTTPService("admin", 3002).
		Build()

	config, err := GenerateFullConfig(request)
	if err != nil {
		log.Fatalf("生成配置失败: %v", err)
	}

	fmt.Println(config)
	fmt.Print("\n")
}

// ExampleMinimalConfig 演示最小化配置
func ExampleMinimalConfig() {
	fmt.Println("=== 示例5: 最小化配置 ===")

	request := NewConfigRequestBuilder().
		SetDomain("simple.com").
		SetCloudflareToken("token").
		AddHTTPService("www", 8080).
		Build()

	config, err := GenerateFullConfig(request)
	if err != nil {
		log.Fatalf("生成配置失败: %v", err)
	}

	fmt.Println(config)
	fmt.Print("\n")
}

// RunAllExamples 运行所有示例
func RunAllExamples() {
	ExampleUsage()
	ExampleFromWebData()
	ExampleCustomHTTPStrategy()
	ExampleMultipleHTTPServices()
	ExampleMinimalConfig()

	fmt.Println("=== 所有示例执行完成！===")
}

// 可以在 main.go 中调用 RunAllExamples() 来运行所有示例
// 或者单独调用某个示例函数来测试特定功能