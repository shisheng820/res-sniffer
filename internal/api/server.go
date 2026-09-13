package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisheng820/res-sniffer/internal/downloader"
	"github.com/shisheng820/res-sniffer/internal/proxy"
	"github.com/shisheng820/res-sniffer/internal/store"
	"github.com/shisheng820/res-sniffer/pkg/types"
)

// Server API 服务器
type Server struct {
	proxy      *proxy.Proxy
	store      *store.Store
	downloader *downloader.Manager
	port       int
	httpServer *http.Server
	upgrader   websocket.Upgrader
	clients    map[*websocket.Conn]bool
	clientsMu  sync.RWMutex
}

// NewServer 创建 API 服务器
func NewServer(proxy *proxy.Proxy, store *store.Store, downloader *downloader.Manager, port int) *Server {
	s := &Server{
		proxy:      proxy,
		store:      store,
		downloader: downloader,
		port:       port,
		clients:    make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源
			},
		},
	}

	// 设置下载回调，推送 WebSocket
	downloader.SetCallback(func(task *types.DownloadTask) {
		s.broadcast(types.WSMessage{
			Type: "download_update",
			Data: task,
		})
	})

	return s
}

// Start 启动服务器
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API 路由
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/proxy/start", s.handleProxyStart)
	mux.HandleFunc("/api/proxy/stop", s.handleProxyStop)
	mux.HandleFunc("/api/proxy/cert", s.handleProxyCert)
	mux.HandleFunc("/api/resources", s.handleResources)
	mux.HandleFunc("/api/resources/", s.handleResourceByID)
	mux.HandleFunc("/api/downloads", s.handleDownloads)
	mux.HandleFunc("/api/downloads/", s.handleDownloadByID)
	mux.HandleFunc("/ws", s.handleWebSocket)

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	log.Printf("API server starting on port %d", s.port)
	return s.httpServer.ListenAndServe()
}

// Stop 停止服务器
func (s *Server) Stop() error {
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

// BroadcastResource 广播新资源
func (s *Server) BroadcastResource(resource *types.Resource) {
	s.broadcast(types.WSMessage{
		Type: "resource_new",
		Data: resource,
	})
}

// handleStatus 获取系统状态
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	totalReq, totalRes := s.proxy.GetStats()
	status := types.ProxyStatus{
		Running:        s.proxy.IsRunning(),
		Address:        "127.0.0.1",
		Port:           s.proxy.GetPort(),
		TotalRequests:  totalReq,
		TotalResources: totalRes,
	}
	s.writeJSON(w, http.StatusOK, status)
}

// handleProxyStart 启动代理
func (s *Server) handleProxyStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req types.StartProxyRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	if err := s.proxy.Start(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"running": true,
		"port":    s.proxy.GetPort(),
		"message": "proxy started",
	})
}

// handleProxyStop 停止代理
func (s *Server) handleProxyStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if err := s.proxy.Stop(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"running": false,
		"message": "proxy stopped",
	})
}

// handleProxyCert 获取 CA 证书
func (s *Server) handleProxyCert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	certPEM := s.proxy.GetCertManager().GetCACertPEM()
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", "attachment; filename=res-sniffer-ca.crt")
	w.Write(certPEM)
}

// handleResources 资源列表
func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listResources(w, r)
	case http.MethodDelete:
		s.store.ClearResources()
		s.writeJSON(w, http.StatusOK, map[string]string{"message": "all resources cleared"})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listResources(w http.ResponseWriter, r *http.Request) {
	resourceType := types.ResourceType(r.URL.Query().Get("type"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	resources, total := s.store.ListResources(resourceType, page, pageSize)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": resources,
		"total": total,
		"page":  page,
	})
}

// handleResourceByID 单个资源操作
func (s *Server) handleResourceByID(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/resources/"):]

	switch r.Method {
	case http.MethodGet:
		resource, ok := s.store.GetResource(id)
		if !ok {
			s.writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		s.writeJSON(w, http.StatusOK, resource)
	case http.MethodDelete:
		if !s.store.DeleteResource(id) {
			s.writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"message": "resource deleted"})
	case http.MethodPost:
		// 从资源创建下载任务
		s.createDownloadFromResource(w, r, id)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) createDownloadFromResource(w http.ResponseWriter, r *http.Request, resourceID string) {
	resource, ok := s.store.GetResource(resourceID)
	if !ok {
		s.writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	var req types.CreateDownloadRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	task, err := s.downloader.CreateTask(resource, req.SaveDir, req.Threads)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.downloader.StartTask(task.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, task)
}

// handleDownloads 下载任务列表
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := types.DownloadStatus(r.URL.Query().Get("status"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

		tasks, total := s.store.ListDownloads(status, page, pageSize)
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": tasks,
			"total": total,
		})
	case http.MethodPost:
		// 创建下载任务（从 URL）
		var req types.CreateDownloadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.URL == "" {
			s.writeError(w, http.StatusBadRequest, "url is required")
			return
		}

		task, err := s.downloader.CreateTaskFromURL(req.URL, req.FileName, req.SaveDir, req.Threads, nil)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if err := s.downloader.StartTask(task.ID); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.writeJSON(w, http.StatusCreated, task)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleDownloadByID 单个下载任务操作
func (s *Server) handleDownloadByID(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/downloads/"):]

	// 处理子路径：/api/downloads/{id}/start, /pause, /resume, /cancel
	if idx := indexOf(id, '/'); idx >= 0 {
		taskID := id[:idx]
		action := id[idx+1:]
		s.handleDownloadAction(w, r, taskID, action)
		return
	}

	switch r.Method {
	case http.MethodGet:
		task, ok := s.downloader.GetTask(id)
		if !ok {
			s.writeError(w, http.StatusNotFound, "download not found")
			return
		}
		s.writeJSON(w, http.StatusOK, task)
	case http.MethodDelete:
		s.downloader.CancelTask(id)
		s.store.DeleteDownload(id)
		s.writeJSON(w, http.StatusOK, map[string]string{"message": "download deleted"})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDownloadAction(w http.ResponseWriter, r *http.Request, taskID, action string) {
	var err error
	switch action {
	case "start":
		err = s.downloader.StartTask(taskID)
	case "pause":
		err = s.downloader.PauseTask(taskID)
	case "resume":
		err = s.downloader.ResumeTask(taskID)
	case "cancel":
		err = s.downloader.CancelTask(taskID)
	default:
		s.writeError(w, http.StatusBadRequest, "unknown action: "+action)
		return
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	task, _ := s.downloader.GetTask(taskID)
	s.writeJSON(w, http.StatusOK, task)
}

// handleWebSocket WebSocket 连接
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	s.clientsMu.Lock()
	s.clients[conn] = true
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, conn)
		s.clientsMu.Unlock()
		conn.Close()
	}()

	// 读取消息（保持连接）
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// broadcast 广播消息给所有 WebSocket 客户端
func (s *Server) broadcast(msg types.WSMessage) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
		}
	}
}

// writeJSON 写入 JSON 响应
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(types.APIResponse{
		Code:    status,
		Message: "success",
		Data:    data,
	})
}

// writeError 写入错误响应
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(types.APIResponse{
		Code:    status,
		Message: message,
	})
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
