package webui

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

// CloudflareAPI 封装 Cloudflare API 调用
type CloudflareAPI struct {
	APIKey    string
	AccountID string
	BaseURL   string
}

// NewCloudflareAPI 创建 Cloudflare API 客户端
func NewCloudflareAPI(apiKey, accountID string) *CloudflareAPI {
	return &CloudflareAPI{
		APIKey:    apiKey,
		AccountID: accountID,
		BaseURL:   "https://api.cloudflare.com/client/v4",
	}
}

// VerifyCredentials 验证 Cloudflare 凭据是否有效
func (c *CloudflareAPI) VerifyCredentials() (bool, string, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/user/tokens/verify", nil)
	if err != nil {
		return false, "", fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, "", fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return false, string(body), fmt.Errorf("验证失败: %s", resp.Status)
	}

	return true, "凭据验证成功", nil
}

// GetZoneID 根据域名获取 Zone ID
func (c *CloudflareAPI) GetZoneID(domain string) (string, error) {
	// 提取根域名
	rootDomain := extractRootDomain(domain)

	url := fmt.Sprintf("%s/zones?name=%s", c.BaseURL, rootDomain)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	var result struct {
		Success bool `json:"success"`
		Result  []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if !result.Success {
		if len(result.Errors) > 0 {
			return "", fmt.Errorf("获取 Zone ID 失败: %s", result.Errors[0].Message)
		}
		return "", fmt.Errorf("获取 Zone ID 失败")
	}

	if len(result.Result) == 0 {
		return "", fmt.Errorf("未找到域名 %s 对应的 Zone", rootDomain)
	}

	return result.Result[0].ID, nil
}

// CreateARecord 创建 A 记录
func (c *CloudflareAPI) CreateARecord(zoneID, domain, ip string) error {
	payload := map[string]interface{}{
		"type":    "A",
		"name":    domain,
		"content": ip,
		"ttl":     1, // 自动 TTL
		"proxied": false,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求数据失败: %v", err)
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records", c.BaseURL, zoneID)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	var result struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if !result.Success {
		if len(result.Errors) > 0 {
			return fmt.Errorf("创建 A 记录失败: %s", result.Errors[0].Message)
		}
		return fmt.Errorf("创建 A 记录失败")
	}

	return nil
}

// GetExistingRecords 获取现有 DNS 记录
func (c *CloudflareAPI) GetExistingRecords(zoneID string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?type=A", c.BaseURL, zoneID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result struct {
		Success bool                      `json:"success"`
		Result  []map[string]interface{} `json:"result"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if !result.Success {
		if len(result.Errors) > 0 {
			return nil, fmt.Errorf("获取 DNS 记录失败: %s", result.Errors[0].Message)
		}
		return nil, fmt.Errorf("获取 DNS 记录失败")
	}

	return result.Result, nil
}

// extractRootDomain 提取根域名
func extractRootDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// ValidateDomain 验证域名格式
func ValidateDomain(domain string) bool {
	if domain == "" {
		return false
	}
	// 简单的域名验证
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}