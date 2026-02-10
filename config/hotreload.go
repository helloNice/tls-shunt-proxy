package config

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// HotReloadManager 热重载管理器
type HotReloadManager struct {
	listener           net.Listener
	connCount          int
	connCountMu        sync.RWMutex
	shuttingDown       bool
	shutdownMu         sync.RWMutex
	wg                 sync.WaitGroup
	newProcessWaitTime time.Duration // 新进程启动等待时间
	gracefulTimeout    time.Duration // 优雅关闭超时时间
}

// NewHotReloadManager 创建热重载管理器
// 支持通过环境变量配置超时时间：
// - TLS_SHUNT_NEW_PROCESS_WAIT_TIME: 新进程等待时间（秒），默认 3
// - TLS_SHUNT_GRACEFUL_TIMEOUT: 优雅关闭超时时间（秒），默认 60
func NewHotReloadManager() *HotReloadManager {
	// 从环境变量读取配置，如果未设置则使用默认值
	newProcessWaitTime := 3 * time.Second
	if waitTimeStr := os.Getenv("TLS_SHUNT_NEW_PROCESS_WAIT_TIME"); waitTimeStr != "" {
		if waitTime, err := strconv.Atoi(waitTimeStr); err == nil && waitTime > 0 {
			newProcessWaitTime = time.Duration(waitTime) * time.Second
			log.Printf("配置新进程等待时间: %d 秒", waitTime)
		}
	}

	gracefulTimeout := 60 * time.Second
	if timeoutStr := os.Getenv("TLS_SHUNT_GRACEFUL_TIMEOUT"); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil && timeout > 0 {
			gracefulTimeout = time.Duration(timeout) * time.Second
			log.Printf("配置优雅关闭超时: %d 秒", timeout)
		}
	}

	return &HotReloadManager{
		connCount:          0,
		newProcessWaitTime: newProcessWaitTime,
		gracefulTimeout:    gracefulTimeout,
	}
}

// SetListener 设置监听器
func (h *HotReloadManager) SetListener(ln net.Listener) {
	h.listener = ln
}

// HasListener 检查是否有监听器
func (h *HotReloadManager) HasListener() bool {
	return h.listener != nil
}

// IncrementConnection 增加连接计数
func (h *HotReloadManager) IncrementConnection() {
	h.connCountMu.Lock()
	h.connCount++
	h.connCountMu.Unlock()
	h.wg.Add(1)
}

// DecrementConnection 减少连接计数
func (h *HotReloadManager) DecrementConnection() {
	h.connCountMu.Lock()
	h.connCount--
	h.connCountMu.Unlock()
	h.wg.Done()
}

// GetConnectionCount 获取当前连接数
func (h *HotReloadManager) GetConnectionCount() int {
	h.connCountMu.RLock()
	defer h.connCountMu.RUnlock()
	return h.connCount
}

// IsShuttingDown 检查是否正在关闭
func (h *HotReloadManager) IsShuttingDown() bool {
	h.shutdownMu.RLock()
	defer h.shutdownMu.RUnlock()
	return h.shuttingDown
}

// SetShuttingDown 设置关闭状态
func (h *HotReloadManager) SetShuttingDown(shuttingDown bool) {
	h.shutdownMu.Lock()
	defer h.shutdownMu.Unlock()
	h.shuttingDown = shuttingDown
}

// GracefulShutdown 优雅关闭，等待所有连接完成
func (h *HotReloadManager) GracefulShutdown(timeout time.Duration) error {
	h.SetShuttingDown(true)
	log.Printf("开始优雅关闭，当前活跃连接数: %d", h.GetConnectionCount())

	// 停止接受新连接
	if h.listener != nil {
		if err := h.listener.Close(); err != nil {
			log.Printf("关闭监听器失败: %v", err)
		}
	}

	// 等待所有连接完成或超时
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("所有连接已关闭，优雅关闭完成")
		return nil
	case <-time.After(timeout):
		log.Printf("优雅关闭超时，强制关闭剩余 %d 个连接", h.GetConnectionCount())
		return fmt.Errorf("优雅关闭超时")
	}
}

// ZeroDowntimeReload 零停机重载配置
//
// 此方法实现真正的零停机重载，通过以下步骤：
// 1. 停止接受新连接
// 2. 获取监听器的文件描述符
// 3. 启动新进程，继承监听器文件描述符
// 4. 等待新进程启动并接管
// 5. 优雅关闭旧进程，等待现有连接完成
//
// 与 Reload() 的区别：
// - Reload(): 在当前进程中重新加载配置，不重启进程（仅用于非关键配置）
// - ZeroDowntimeReload(): 启动新进程并优雅关闭旧进程，真正的零停机重载
//
// 使用场景：
// - 需要 SSL 证书更新（监听器端口变更）
// - 需要监听地址变更
// - 需要确保现有连接不中断
func (h *HotReloadManager) ZeroDowntimeReload(configPath string) error {
	log.Println("开始零停机配置重载...")

	// 1. 设置为关闭状态，停止接受新连接
	h.SetShuttingDown(true)
	log.Println("已停止接受新连接")

	// 2. 获取监听器的文件描述符
	tcpListener, ok := h.listener.(*net.TCPListener)
	if !ok {
		// 如果不是 TCP 监听器，回退到普通重载
		log.Println("监听器不是 TCP 类型，回退到普通重载")
		h.SetShuttingDown(false)
		return fmt.Errorf("监听器不是 TCP 类型")
	}

	file, err := tcpListener.File()
	if err != nil {
		// 获取文件描述符失败，回退到普通重载
		log.Printf("获取监听器文件描述符失败: %v，回退到普通重载", err)
		h.SetShuttingDown(false)
		return fmt.Errorf("获取监听器文件描述符失败: %w", err)
	}
	defer file.Close()

	listenFD := file.Fd()
	log.Printf("获取到监听器文件描述符: %d", listenFD)

	// 3. 启动新进程，继承监听器文件描述符
	execPath, err := os.Executable()
	if err != nil {
		// 回滚：恢复接受新连接
		log.Printf("获取可执行文件路径失败: %v，回滚", err)
		h.SetShuttingDown(false)
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	// 设置环境变量，传递文件描述符编号
	env := append(os.Environ(),
		fmt.Sprintf("TLS_SHUNT_LISTENER_FD=%d", listenFD),
		fmt.Sprintf("TLS_SHUNT_RELOAD_MODE=zero-downtime"),
	)

	cmd := exec.Command(execPath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	cmd.ExtraFiles = []*os.File{file} // 传递文件描述符

	log.Printf("启动新进程: %s", execPath)
	if err := cmd.Start(); err != nil {
		// 回滚：恢复接受新连接
		log.Printf("启动新进程失败: %v，回滚", err)
		h.SetShuttingDown(false)
		return fmt.Errorf("启动新进程失败: %w", err)
	}

	// 4. 等待新进程启动并接管
	log.Printf("等待新进程启动（最多 %v）...", h.newProcessWaitTime)
	time.Sleep(h.newProcessWaitTime)

	// 5. 检查新进程是否正在运行
	if cmd.Process == nil {
		// 回滚：恢复接受新连接
		log.Println("新进程未启动，回滚")
		h.SetShuttingDown(false)
		return fmt.Errorf("新进程未启动")
	}

	// 检查新进程是否仍在运行
	if !isProcessRunning(cmd.Process.Pid) {
		// 回滚：恢复接受新连接
		log.Printf("新进程 (PID: %d) 已退出，回滚", cmd.Process.Pid)
		h.SetShuttingDown(false)
		return fmt.Errorf("新进程启动后立即退出")
	}

	log.Printf("新进程已成功启动 (PID: %d)，正在优雅关闭旧进程...", cmd.Process.Pid)

	// 6. 等待所有现有连接完成（带超时）
	connCount := h.GetConnectionCount()
	if connCount > 0 {
		log.Printf("等待 %d 个现有连接完成（最多 %v）...", connCount, h.gracefulTimeout)
		if err := h.GracefulShutdown(h.gracefulTimeout); err != nil {
			log.Printf("优雅关闭失败: %v，强制退出", err)
		}
	} else {
		log.Println("没有活跃连接，立即退出")
	}

	// 7. 退出旧进程
	log.Println("旧进程退出")
	os.Exit(0)
	return nil
}

// isProcessRunning 检查进程是否正在运行
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// 在 Windows 上，os.FindProcess 不会检查进程是否存在
	// 我们需要使用其他方法
	// 由于 Windows 不支持 Unix 的 Signal(0) 机制，
	// 我们使用一个简化的方法：通过等待进程来判断
	
	// 创建一个带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 尝试等待进程，带超时
	done := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		done <- err
	}()

	select {
	case <-done:
		// 进程已经退出
		return false
	case <-ctx.Done():
		// 超时，进程可能仍在运行
		return true
	}
}

// CanAcceptConnection 检查是否可以接受新连接
func (h *HotReloadManager) CanAcceptConnection() bool {
	if h.IsShuttingDown() {
		return false
	}
	return true
}