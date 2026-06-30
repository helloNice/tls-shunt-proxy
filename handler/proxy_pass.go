package handler

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
)

type ProxyPassHandler struct {
	target        string
	proxyProtocol bool
}

var inboundBufferPool, outboundBufferPool *sync.Pool

func InitBufferPools(inboundBufferSize, outboundBufferSize int) {
	inboundBufferPool = newBufferPool(inboundBufferSize)
	outboundBufferPool = newBufferPool(outboundBufferSize)
}

func newBufferPool(size int) *sync.Pool {
	return &sync.Pool{New: func() interface{} {
		return make([]byte, size)
	}}
}

func NewProxyPassHandler(args string) *ProxyPassHandler {
	handler := ProxyPassHandler{}
	parts := strings.Split(args, ";")
	handler.target = parts[0]
	for _, arg := range parts {
		arg = strings.TrimSpace(arg)
		if arg == "proxyProtocol" {
			handler.proxyProtocol = true
		}
	}
	return &handler
}

func (h *ProxyPassHandler) Handle(conn net.Conn) {
	if conn == nil {
		log.Printf("[DEBUG] 转发失败: 连接为空\n")
		return
	}

	defer conn.Close()

	var err error
	var dstConn net.Conn

	log.Printf("[DEBUG] 开始转发: 来源=%s，目标=%s，ProxyProtocol=%v\n",
		conn.RemoteAddr().String(), h.target, h.proxyProtocol)

	if strings.HasPrefix(h.target, "unix:") {
		dstConn, err = net.Dial("unix", h.target[5:])
	} else {
		dstConn, err = net.Dial("tcp", h.target)
	}
	if err != nil {
		log.Printf("[DEBUG] 转发失败: 无法连接到目标 %s，错误类型=%v\n", h.target, err)
		if strings.Contains(err.Error(), "timeout") {
			log.Printf("[DEBUG] 转发失败原因: 连接目标服务器超时\n")
		} else if strings.Contains(err.Error(), "refused") {
			log.Printf("[DEBUG] 转发失败原因: 目标服务器拒绝连接\n")
		} else if strings.Contains(err.Error(), "no route") {
			log.Printf("[DEBUG] 转发失败原因: 无法路由到目标服务器\n")
		} else if strings.Contains(err.Error(), "connection reset") {
			log.Printf("[DEBUG] 转发失败原因: 连接被目标服务器重置\n")
		} else {
			log.Printf("[DEBUG] 转发失败原因: 目标服务器未响应或网络问题\n")
		}
		log.Printf("fail to connect to %s :%v\n", h.target, err)
		return
	}
	defer dstConn.Close()

	log.Printf("[DEBUG] 转发成功: 已建立连接到目标 %s (%s)\n",
		h.target, dstConn.RemoteAddr().String())

	if h.proxyProtocol {
		if err := h.sendProxyProtocol(conn, dstConn); err != nil {
			log.Printf("[DEBUG] 发送 Proxy Protocol 失败: %v\n", err)
			log.Printf("[DEBUG] 转发失败原因: Proxy Protocol 发送失败\n")
			log.Printf("fail to send proxy %s :%v\n", h.target, err)
			return
		}
		log.Printf("[DEBUG] Proxy Protocol 已发送\n")
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go doCopy(dstConn, conn, inboundBufferPool, &wg)
	go doCopy(conn, dstConn, outboundBufferPool, &wg)

	wg.Wait()

	log.Printf("[DEBUG] 转发完成: 来源=%s，目标=%s\n", conn.RemoteAddr().String(), h.target)
}

func (h *ProxyPassHandler) sendProxyProtocol(srcConn, dstConn net.Conn) error {
	remoteAddr, remotePort, err := net.SplitHostPort(srcConn.RemoteAddr().String())
	if err != nil {
		return err
	}
	localAddr, localPort, err := net.SplitHostPort(srcConn.LocalAddr().String())
	if err != nil {
		return err
	}

	ipVer := "4"
	if strings.Contains(remoteAddr, ":") {
		ipVer = "6"
	}
	_, err = fmt.Fprintf(dstConn, "PROXY TCP%s %s %s %s %s\r\n", ipVer, remoteAddr, localAddr, remotePort, localPort)
	return err
}

func doCopy(dst io.Writer, src io.Reader, bufferPool *sync.Pool, wg *sync.WaitGroup) {
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)
	bytesCopied, err := io.CopyBuffer(dst, src, buf)
	if err != nil && err != io.EOF {
		log.Printf("[DEBUG] 数据传输失败: 传输字节数=%d，错误=%v\n", bytesCopied, err)
		log.Printf("[DEBUG] 转发失败原因: 目标服务器可能未响应或连接已断开\n")
		log.Printf("failed to proxy pass: %v\n", err)
	} else {
		log.Printf("[DEBUG] 数据传输成功: 传输字节数=%d\n", bytesCopied)
	}
	wg.Done()
}
