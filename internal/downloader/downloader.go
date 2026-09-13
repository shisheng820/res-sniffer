package downloader

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisheng820/res-sniffer/pkg/types"
)

// DownloadCallback 下载进度回调
type DownloadCallback func(task *types.DownloadTask)

// Manager 下载管理器
type Manager struct {
	tasks     map[string]*types.DownloadTask
	mu        sync.RWMutex
	callback  DownloadCallback
	saveDir   string
	maxThreads int
	client    *http.Client
	stopChans map[string]chan struct{}
}

// NewManager 创建下载管理器
func NewManager(saveDir string, maxThreads int) *Manager {
	if saveDir == "" {
		home, _ := os.UserHomeDir()
		saveDir = filepath.Join(home, "Downloads", "res-sniffer")
	}
	os.MkdirAll(saveDir, 0755)

	if maxThreads <= 0 {
		maxThreads = 4
	}

	return &Manager{
		tasks:      make(map[string]*types.DownloadTask),
		saveDir:    saveDir,
		maxThreads: maxThreads,
		client: &http.Client{
			Timeout: 0, // 不超时
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		stopChans: make(map[string]chan struct{}),
	}
}

// SetCallback 设置回调
func (m *Manager) SetCallback(cb DownloadCallback) {
	m.callback = cb
}

// CreateTask 创建下载任务
func (m *Manager) CreateTask(resource *types.Resource, saveDir string, threads int) (*types.DownloadTask, error) {
	if threads <= 0 {
		threads = m.maxThreads
	}

	if saveDir == "" {
		saveDir = m.saveDir
	}
	os.MkdirAll(saveDir, 0755)

	taskID := fmt.Sprintf("dl_%d", time.Now().UnixNano())
	fileName := resource.FileName
	savePath := filepath.Join(saveDir, fileName)

	// 避免文件名冲突
	savePath = ensureUniquePath(savePath)

	task := &types.DownloadTask{
		ID:           taskID,
		ResourceID:   resource.ID,
		URL:          resource.URL,
		FileName:     fileName,
		SavePath:     savePath,
		TotalSize:    resource.FileSize,
		Status:       types.DownloadPending,
		Threads:      threads,
		ResourceType: resource.Type,
		Headers:      resource.Headers,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	m.stopChans[taskID] = make(chan struct{})
	m.mu.Unlock()

	return task, nil
}

// CreateTaskFromURL 从 URL 创建下载任务
func (m *Manager) CreateTaskFromURL(url, fileName, saveDir string, threads int, headers map[string]string) (*types.DownloadTask, error) {
	resource := &types.Resource{
		URL:      url,
		FileName: fileName,
		Headers:  headers,
		Type:     detectTypeFromURL(url),
	}
	return m.CreateTask(resource, saveDir, threads)
}

// StartTask 开始下载
func (m *Manager) StartTask(taskID string) error {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	stopChan := m.stopChans[taskID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status == types.DownloadRunning {
		return fmt.Errorf("task already running")
	}

	task.Status = types.DownloadRunning
	m.notify(task)

	// 根据资源类型选择下载方式
	if task.ResourceType == types.ResourceTypeM3U8 {
		go m.downloadM3U8(task, stopChan)
	} else {
		go m.downloadFile(task, stopChan)
	}

	return nil
}

// PauseTask 暂停下载
func (m *Manager) PauseTask(taskID string) error {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status != types.DownloadRunning {
		return fmt.Errorf("task is not running")
	}

	task.Status = types.DownloadPaused
	m.notify(task)
	return nil
}

// ResumeTask 恢复下载
func (m *Manager) ResumeTask(taskID string) error {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if task.Status != types.DownloadPaused {
		return fmt.Errorf("task is not paused")
	}

	task.Status = types.DownloadRunning
	m.notify(task)

	// 重新启动下载（简单实现：从头开始或断点续传）
	if task.ResourceType == types.ResourceTypeM3U8 {
		go m.downloadM3U8(task, m.stopChans[taskID])
	} else {
		go m.downloadFile(task, m.stopChans[taskID])
	}

	return nil
}

// CancelTask 取消下载
func (m *Manager) CancelTask(taskID string) error {
	m.mu.Lock()
	task, ok := m.tasks[taskID]
	if ok {
		if ch, exists := m.stopChans[taskID]; exists {
			close(ch)
			delete(m.stopChans, taskID)
		}
		task.Status = types.DownloadFailed
		task.Error = "cancelled by user"
	}
	m.mu.Unlock()

	if ok {
		m.notify(task)
	}
	return nil
}

// GetTask 获取任务
func (m *Manager) GetTask(taskID string) (*types.DownloadTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[taskID]
	return task, ok
}

// ListTasks 列出所有任务
func (m *Manager) ListTasks() []*types.DownloadTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tasks := make([]*types.DownloadTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// downloadFile 普通文件下载（支持多线程）
func (m *Manager) downloadFile(task *types.DownloadTask, stopChan chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			task.Status = types.DownloadFailed
			task.Error = fmt.Sprintf("panic: %v", r)
			m.notify(task)
		}
	}()

	// 先发送 HEAD 请求获取文件大小
	if task.TotalSize <= 0 {
		size, err := m.getFileSize(task)
		if err == nil {
			task.TotalSize = size
		}
	}

	// 单线程下载（简单可靠）
	req, err := http.NewRequest("GET", task.URL, nil)
	if err != nil {
		task.Status = types.DownloadFailed
		task.Error = err.Error()
		m.notify(task)
		return
	}

	// 设置请求头
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := m.client.Do(req)
	if err != nil {
		task.Status = types.DownloadFailed
		task.Error = err.Error()
		m.notify(task)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		task.Status = types.DownloadFailed
		task.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		m.notify(task)
		return
	}

	// 创建文件
	out, err := os.Create(task.SavePath)
	if err != nil {
		task.Status = types.DownloadFailed
		task.Error = err.Error()
		m.notify(task)
		return
	}
	defer out.Close()

	// 下载并统计进度
	buf := make([]byte, 32*1024)
	var downloaded int64
	startTime := time.Now()
	lastUpdate := startTime

	for {
		// 检查暂停/取消
		m.mu.RLock()
		currentTask := m.tasks[task.ID]
		paused := currentTask.Status == types.DownloadPaused
		m.mu.RUnlock()

		if paused {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		select {
		case <-stopChan:
			task.Status = types.DownloadFailed
			task.Error = "cancelled"
			m.notify(task)
			return
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				task.Status = types.DownloadFailed
				task.Error = werr.Error()
				m.notify(task)
				return
			}
			downloaded += int64(n)
			atomic.StoreInt64(&task.Downloaded, downloaded)

			// 更新速度和进度（每秒更新一次）
			now := time.Now()
			if now.Sub(lastUpdate) >= time.Second {
				elapsed := now.Sub(startTime).Seconds()
				if elapsed > 0 {
					task.Speed = int64(float64(downloaded) / elapsed)
				}
				m.notify(task)
				lastUpdate = now
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			task.Status = types.DownloadFailed
			task.Error = err.Error()
			m.notify(task)
			return
		}
	}

	task.Downloaded = downloaded
	task.Status = types.DownloadCompleted
	task.Speed = 0
	m.notify(task)
}

// getFileSize 获取文件大小
func (m *Manager) getFileSize(task *types.DownloadTask) (int64, error) {
	req, err := http.NewRequest("HEAD", task.URL, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.ContentLength, nil
}

// notify 通知回调
func (m *Manager) notify(task *types.DownloadTask) {
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
	if m.callback != nil {
		m.callback(task)
	}
}

// ensureUniquePath 确保路径唯一
func ensureUniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 1; ; i++ {
		newPath := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}

// detectTypeFromURL 从 URL 检测类型
func detectTypeFromURL(url string) types.ResourceType {
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".m3u8"):
		return types.ResourceTypeM3U8
	case strings.HasSuffix(lower, ".mp4"), strings.HasSuffix(lower, ".webm"),
		strings.HasSuffix(lower, ".avi"), strings.HasSuffix(lower, ".mkv"),
		strings.HasSuffix(lower, ".mov"), strings.HasSuffix(lower, ".flv"):
		return types.ResourceTypeVideo
	case strings.HasSuffix(lower, ".mp3"), strings.HasSuffix(lower, ".wav"),
		strings.HasSuffix(lower, ".flac"), strings.HasSuffix(lower, ".aac"),
		strings.HasSuffix(lower, ".ogg"), strings.HasSuffix(lower, ".m4a"):
		return types.ResourceTypeAudio
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"),
		strings.HasSuffix(lower, ".png"), strings.HasSuffix(lower, ".gif"),
		strings.HasSuffix(lower, ".webp"):
		return types.ResourceTypeImage
	default:
		return types.ResourceTypeOther
	}
}
