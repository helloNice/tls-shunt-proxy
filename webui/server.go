package webui

import (
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
	"github.com/liberal-boy/tls-shunt-proxy/config/raw"
	"gopkg.in/yaml.v2"
)

// Start 启动 web 管理界面（在新 goroutine 中），configPath 为配置文件路径
// 绑定到 127.0.0.1:8080，启用 Basic Auth（凭据来自环境变量 WEBUI_USER/WEBUI_PASS）
func Start(configPath string) {
	tmpl := template.Must(template.ParseFiles("webui/templates/index.html"))

	mux := http.NewServeMux()

	// API: 获取当前配置
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "仅支持 GET", http.StatusMethodNotAllowed)
			return
		}
		data, err := ioutil.ReadFile(configPath)
		if err != nil {
			// 如果文件不存在或读取失败，返回空配置而不是错误
			log.Printf("读取配置文件失败: %v，将返回空配置", err)
			data = []byte("")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"config": string(data)})
	})

	// API: 验证 Cloudflare 凭据
	mux.HandleFunc("/api/validate-cloudflare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			APIKey    string `json:"api_key"`
			AccountID string `json:"account_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
			return
		}

		api := NewCloudflareAPI(req.APIKey, req.AccountID)
		valid, message, err := api.VerifyCredentials()
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"valid":   false,
				"message": err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":   valid,
			"message": message,
		})
	})

	// API: 生成配置
	mux.HandleFunc("/api/generate-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
			return
		}
		var formData raw.WizardFormData
		if err := json.NewDecoder(r.Body).Decode(&formData); err != nil {
			http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
			return
		}

		config := generateConfigFromWizard(&formData)
		yamlData, err := yaml.Marshal(config)
		if err != nil {
			http.Error(w, "生成配置失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"config": string(yamlData)})
	})

	// API: 获取支持的架构列表
	mux.HandleFunc("/api/architectures", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "仅支持 GET", http.StatusMethodNotAllowed)
			return
		}
		architectures := []map[string]string{
			{"value": "amd64", "label": "AMD64 (Intel/AMD 64位)", "description": "适用于大多数现代服务器"},
			{"value": "arm64", "label": "ARM64 (64位 ARM)", "description": "适用于树莓派 4+、Apple Silicon 等"},
			{"value": "386", "label": "386 (Intel 32位)", "description": "适用于老旧 32位系统"},
			{"value": "arm", "label": "ARM (32位)", "description": "适用于树莓派 1-3 等"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(architectures)
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
		if err := r.ParseForm(); err != nil {
			http.Error(w, "解析表单失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		yamlText := r.FormValue("config")
		// 基本语法校验
		var tmp interface{}
		if err := yaml.Unmarshal([]byte(yamlText), &tmp); err != nil {
			http.Error(w, "YAML 校验失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		// 原子写入
		tmpFile := configPath + ".tmp"
		if err := ioutil.WriteFile(tmpFile, []byte(yamlText), 0644); err != nil {
			http.Error(w, "写入临时文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmpFile, configPath); err != nil {
			http.Error(w, "替换配置文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// 启动新进程并退出当前进程 -> 实现 reload
		execPath, err := os.Executable()
		if err != nil {
			http.Error(w, "获取可执行文件路径失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		cmd := exec.Command(execPath, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			http.Error(w, "启动新进程失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "保存成功，正在重启服务以应用新配置...\n")
		go func() {
			_ = cmd.Process.Release()
			os.Exit(0)
		}()
	})

	// 包装 Basic Auth：从环境变量读取 WEBUI_USER / WEBUI_PASS，默认 admin/admin
	handler := basicAuthMiddleware(mux)

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
		if r.Header.Get("Authorization") != expected {
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