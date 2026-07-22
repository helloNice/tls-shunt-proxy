package handler

import (
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"log"
	"net"
	"net/http"
	"sync"
)

type FileServerHandler struct {
	connListener *ConnListener
}

type trackedConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func newTrackedConn(conn net.Conn) *trackedConn {
	return &trackedConn{
		Conn: conn,
		done: make(chan struct{}),
	}
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		close(c.done)
	})
	return err
}

func NewFileServerHandler(path string) *FileServerHandler {
	ln := NewConnListener()
	go func() {
		h2s := &http2.Server{}
		fileServer := http.FileServer(http.Dir(path))
		withGzip := DefaultGzipHandler().WrapHandler(fileServer)
		server := &http.Server{
			Handler: h2c.NewHandler(withGzip, h2s),
		}
		server.SetKeepAlivesEnabled(false)
		err := server.Serve(ln)
		if err != nil {
			log.Fatalln(err)
		}
	}()
	return &FileServerHandler{connListener: ln}
}

func (h *FileServerHandler) Handle(conn net.Conn) {
	tracked := newTrackedConn(conn)
	err := h.connListener.HandleConn(tracked)
	if err != nil {
		log.Println(err)
		_ = tracked.Close()
		return
	}
	<-tracked.done
}
