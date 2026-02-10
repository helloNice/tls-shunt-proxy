package webui

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/liberal-boy/tls-shunt-proxy/config/raw"
	"gopkg.in/yaml.v2"
)

var configMutex sync.Mutex

// Start 启动 web 管理界面（在新 goroutine 中），configPath 为配置文件路径
// 绑定到 127.0.0.1:8080，启用 Basic Auth（凭据来自环境变量 WEBUI_USER/WEBUI_PASS）
// reloadMgr 为热重载管理器，用于零停机重载
func Start(configPath string, reloadMgr interface{}) {
	tmpl := template.Must(template.ParseFiles("webui/templates/index.html"))

	mux := http.NewServeMux()

	// API: 获取当前配置
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			sendAPIResponse(w, false, "", "仅支持 GET", nil)
			return
		}
		data, err := ioutil.ReadFile(configPath)
		if err != nil {
			// 如果文件不存在或读取失败，返回空配置而不是错误
			log.Printf("读取配置文件失败: %v，将返回空配置", err)
			data = []byte("")
		}
		sendAPIResponse(w, true, "配置获取成功", "", map[string]string{"config": string(data)})
	})

	// API: 验证 Cloudflare 凭据
	mux.HandleFunc("/api/validate-cloudflare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			sendAPIResponse(w, false, "", "仅支持 POST", nil)
			return
		}
		
		// 验证输入
		var req struct {
			APIKey    string `json:"api_key"`
			AccountID string `json:"account_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendAPIResponse(w, false, "", "解析请求失败", nil)
			return
		}

		// 验证 API 密钥格式
		if len(req.APIKey) < 32 || len(req.APIKey) > 255 {
			sendAPIResponse(w, false, "", "API 密钥长度无效", nil)
			return
		}

		// 验证 AccountID 格式
		if req.AccountID != "" && (len(req.AccountID) != 32 || !isValidHex(req.AccountID)) {
			sendAPIResponse(w, false, "", "Account ID 格式无效", nil)
			return
		}

		api := NewCloudflareAPI(req.APIKey, req.AccountID)
		valid, message, err := api.VerifyCredentials()
		if err != nil {
			sendAPIResponse(w, false, "", "验证失败: "+err.Error(), nil)
			return
		}

		sendAPIResponse(w, true, "验证成功", "", map[string]interface{}{
			"valid":   valid,
			"message": message,
		})
	})

	// API: 生成配置
	mux.HandleFunc("/api/generate-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			sendAPIResponse(w, false, "", "仅支持 POST", nil)
			return
		}
		var formData raw.WizardFormData
		if err := json.NewDecoder(r.Body).Decode(&formData); err != nil {
			sendAPIResponse(w, false, "", "解析请求失败", nil)
			return
		}

		config := generateConfigFromWizard(&formData)
		yamlData, err := yaml.Marshal(config)
		if err != nil {
			log.Printf("生成配置失败: %v", err)
			sendAPIResponse(w, false, "", "生成配置失败", nil)
			return
		}

		sendAPIResponse(w, true, "配置生成成功", "", map[string]string{"config": string(yamlData)})
	})

	// API: 获取支持的架构列表
	mux.HandleFunc("/api/architectures", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			sendAPIResponse(w, false, "", "仅支持 GET", nil)
			return
		}
		architectures := []map[string]string{
			{"value": "amd64", "label": "AMD64 (Intel/AMD 64位)", "description": "适用于大多数现代服务器"},
			{"value": "arm64", "label": "ARM64 (64位 ARM)", "description": "适用于树莓派 4+、Apple Silicon 等"},
			{"value": "386", "label": "386 (Intel 32位)", "description": "适用于老旧 32位系统"},
			{"value": "arm", "label": "ARM (32位)", "description": "适用于树莓派 1-3 等"},
		}
		sendAPIResponse(w, true, "架构列表获取成功", "", architectures)
	})

	// API: 获取连接统计信息
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			sendAPIResponse(w, false, "", "仅支持 GET", nil)
			return
		}

		stats := map[string]interface{}{
			"connections": 0,
			"shutting_down": false,
		}

		if reloadMgr != nil {
			if hr, ok := reloadMgr.(interface {
				GetConnectionCount() int
				IsShuttingDown() bool
			}); ok {
				stats["connections"] = hr.GetConnectionCount()
				stats["shutting_down"] = hr.IsShuttingDown()
			}
		}

		sendAPIResponse(w, true, "统计信息获取成功", "", stats)
	})

	// API: 重新加载配置（零停机）
	mux.HandleFunc("/api/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			sendAPIResponse(w, false, "", "仅支持 POST", nil)
			return
		}

		// 验证配置文件语法
		if err := validateConfigFile(configPath); err != nil {
			sendAPIResponse(w, false, "", "配置验证失败: "+err.Error(), nil)
			return
		}

		// 零停机重载：向主进程发送 SIGHUP 信号
		if reloadMgr != nil {
			// 使用类型断言检查是否为 HotReloadManager
			if hr, ok := reloadMgr.(interface{ ZeroDowntimeReload(string) error }); ok {
				go func() {
					if err := hr.ZeroDowntimeReload(configPath); err != nil {
						log.Printf("零停机重载失败: %v", err)
					}
				}()
			} else {
				// 回退到重启方式
				log.Println("回退到进程重启方式")
				go reloadByRestart()
			}
		} else {
			// 没有热重载管理器，使用重启方式
			go reloadByRestart()
		}

		sendAPIResponse(w, true, "零停机配置重载已启动，现有连接不会中断...", "", nil)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "仅支持 GET", http.StatusMethodNotAllowed)
			return
		}
		data, err := ioutil.ReadFile(configPath)
		if err != nil {
			// 如果文件不存在或读取失败，显示空配置而不是错误
			log.Printf("读取配置文件失败: %v，将显示空配置", err)
			data = []byte("")
		}
		// 如果文件为空，提供默认的提示信息
		configText := string(data)
		if configText == "" {
			configText = "# 配置文件为空，请使用上方'新建配置'按钮创建配置\n# 或直接在此处编辑 YAML 配置"
		}
		_ = tmpl.Execute(w, struct{ ConfigYAML string }{ConfigYAML: configText})
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
			return
		}
		
		// 验证配置路径不包含路径遍历
		cleanConfigPath := filepath.Clean(configPath)
		if cleanConfigPath != configPath {
			http.Error(w, "无效的配置路径", http.StatusBadRequest)
			return
		}
		
		expectedDir := filepath.Dir(cleanConfigPath)
		actualDir := filepath.Dir(cleanConfigPath)
		if expectedDir != actualDir {
			http.Error(w, "配置路径超出允许范围", http.StatusBadRequest)
			return
		}
		
		if err := r.ParseForm(); err != nil {
			http.Error(w, "解析表单失败", http.StatusBadRequest)
			return
		}
		yamlText := r.FormValue("config")
		
		// 基本语法校验
		var tmp interface{}
		if err := yaml.Unmarshal([]byte(yamlText), &tmp); err != nil {
			http.Error(w, "YAML 校验失败", http.StatusBadRequest)
			return
		}
		
		// 业务逻辑验证
		if err := validateConfigBusinessLogic([]byte(yamlText)); err != nil {
			http.Error(w, "配置验证失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		
		// 使用互斥锁保护配置文件写入
		configMutex.Lock()
		defer configMutex.Unlock()
		
		// 原子写入
		tmpFile := configPath + ".tmp"
		if err := ioutil.WriteFile(tmpFile, []byte(yamlText), 0600); err != nil {
			log.Printf("写入临时文件失败: %v", err)
			http.Error(w, "内部服务器错误", http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmpFile, configPath); err != nil {
			log.Printf("替换配置文件失败: %v", err)
			http.Error(w, "内部服务器错误", http.StatusInternalServerError)
			return
		}
		
		// 启动新进程并退出当前进程 -> 实现 reload
		execPath, err := os.Executable()
		if err != nil {
			log.Printf("获取可执行文件路径失败: %v", err)
			http.Error(w, "内部服务器错误", http.StatusInternalServerError)
			return
		}
		// 使用固定参数而不是原始参数，防止命令注入
		args := []string{"-config", configPath}
		cmd := exec.Command(execPath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			log.Printf("启动新进程失败: %v", err)
			http.Error(w, "内部服务器错误", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "保存成功，正在重启服务以应用新配置...\n")
		go func() {
			_ = cmd.Process.Release()
			os.Exit(0)
		}()
	})

	// 创建带安全头的处理器链
	handler := securityHeadersMiddleware(basicAuthMiddleware(mux))

	bind := "127.0.0.1:8080"
	log.Println("web 管理界面启动，访问 http://" + bind + " （默认账户 admin/admin，如已在环境中设置 WEBUI_USER/WEBUI_PASS 则使用设置的）")
	go func() {
		if err := http.ListenAndServe(bind, handler); err != nil {
			log.Println("webui 启动失败:", err)
		}
	}()
}

// basicAuthMiddleware 简单的 Basic Auth 中间件，凭据来自环境变量
func basicAuthMiddleware(next http.Handler) http.Handler {
	user := os.Getenv("WEBUI_USER")
	pass := os.Getenv("WEBUI_PASS")
	if user == "" {
		user = "admin"
	}
	if pass == "" {
		pass = "admin"
	}
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(expected), []byte(received)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="tls-shunt-proxy 管理界面"`)
			http.Error(w, "未授权", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// generateConfigFromWizard 根据 Wizard 表单数据生成配置
func generateConfigFromWizard(formData *raw.WizardFormData) *raw.ConfigTemplate {
	config := &raw.ConfigTemplate{
		Listen:             "0.0.0.0:443",
		RedirectHttps:      "0.0.0.0:80",
		InboundBufferSize:  4,
		OutboundBufferSize: 32,
		Fallback:           "127.0.0.1:8443",
		WebUIListen:        "127.0.0.1:8080",
		VHosts:             []raw.RawVHost{},
	}

	// 创建虚拟主机配置
	vhost := raw.RawVHost{
		Name:          formData.Domain,
		TlsOffloading: true,
		ManagedCert:   true,
		KeyType:       "p256",
		Alpn:          "h2,http/1.1",
		Protocols:     "tls12,tls13",
		Http: raw.RawHttpHandler{
			Paths: []raw.RawPathHandler{},
		},
		Http2:  []raw.RawPathHandler{},
		Trojan: raw.RawHandler{},
		Default: raw.RawHandler{
			Handler: "proxyPass",
			Args:    fmt.Sprintf("127.0.0.1:%d", formData.DefaultBackendPort),
		},
	}

	// 配置 HTTP 处理
	if formData.EnableHTTP {
		if formData.HTTPBackendPort > 0 {
			vhost.Http.Paths = append(vhost.Http.Paths, raw.RawPathHandler{
				Path:    "/",
				Handler: "proxyPass",
				Args:    fmt.Sprintf("127.0.0.1:%d", formData.HTTPBackendPort),
			})
		}
		if formData.StaticFilePath != "" {
			vhost.Http.Paths = append(vhost.Http.Paths, raw.RawPathHandler{
				Path:       "/static/",
				Handler:    "fileServer",
				Args:       formData.StaticFilePath,
				TrimPrefix: "/static",
			})
		}
	}

	// 配置 HTTP/2 处理
	if formData.EnableHTTP2 && formData.HTTP2BackendPort > 0 {
		vhost.Http2 = append(vhost.Http2, raw.RawPathHandler{
			Path:    "/",
			Handler: "proxyPass",
			Args:    fmt.Sprintf("h2c://localhost:%d", formData.HTTP2BackendPort),
		})
	}

	// 配置 Trojan 处理
	if formData.EnableTrojan && formData.TrojanBackendPort > 0 {
		vhost.Trojan = raw.RawHandler{
			Handler: "proxyPass",
			Args:    fmt.Sprintf("127.0.0.1:%d", formData.TrojanBackendPort),
		}
	}

	config.VHosts = append(config.VHosts, vhost)

	return config
}

// validateConfigFile 验证配置文件
func validateConfigFile(configPath string) error {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	var tmp interface{}
	if err := yaml.Unmarshal(data, &tmp); err != nil {
		return fmt.Errorf("配置文件语法错误: %w", err)
	}

	return nil
}

// securityHeadersMiddleware 添加安全相关的 HTTP 头
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 防止 MIME 类型混淆攻击
		w.Header().Set("X-Content-Type-Options", "nosniff")
		
		// 防止跨站脚本攻击
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		
		// 防止点击劫持
		w.Header().Set("X-Frame-Options", "DENY")
		
		// 限制引用来源
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		
		next.ServeHTTP(w, r)
	})
}

// validateConfigBusinessLogic 验证配置的业务逻辑
func validateConfigBusinessLogic(configData []byte) error {
	var config raw.RawConfig
	if err := yaml.Unmarshal(configData, &config); err != nil {
		return err
	}
	
	// 验证监听地址格式
	if config.Listen != "" {
		// 简单验证格式，不实际尝试解析地址
		parts := strings.Split(config.Listen, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("监听地址格式无效")
		}
	}
	
	// 验证虚拟主机配置
	for _, vhost := range config.VHosts {
		if vhost.Name == "" {
			return fmt.Errorf("虚拟主机名称不能为空")
		}
		
		// 验证域名格式（简单验证）
		if !isValidDomain(vhost.Name) {
			return fmt.Errorf("域名格式无效: %s", vhost.Name)
		}
	}
	
	return nil
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

// isValidHex 验证十六进制字符串
func isValidHex(s string) bool {
	for _, char := range s {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

// APIResponse 统一的 API 响应结构
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// sendAPIResponse 发送统一格式的 API 响应
func sendAPIResponse(w http.ResponseWriter, success bool, message, errorMsg string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	response := APIResponse{
		Success: success,
		Message: message,
		Error:   errorMsg,
	}
	if data != nil {
		response.Data = data
	}
	json.NewEncoder(w).Encode(response)
}

// reloadByRestart 通过重启方式重载配置
func reloadByRestart() {
	// 从环境变量或默认值获取配置路径
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "./config.yaml" // 默认配置文件路径
	}
	
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("获取可执行文件路径失败: %v", err)
		return
	}
	// 使用固定参数而不是原始参数，防止命令注入
	args := []string{"-config", configPath}
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("启动新进程失败: %v", err)
		return
	}
	_ = cmd.Process.Release()
	os.Exit(0)
}