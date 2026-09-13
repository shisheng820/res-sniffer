package store

import (
	"sync"
	"time"

	"github.com/shisheng820/res-sniffer/pkg/types"
)

// Store 数据存储（内存实现）
type Store struct {
	mu         sync.RWMutex
	resources  map[string]*types.Resource
	downloads  map[string]*types.DownloadTask
	// 资源按时间排序的 ID 列表，用于分页
	resourceIDs []string
	downloadIDs []string
	// 最大保留数量
	maxResources int
	maxDownloads int
}

// NewStore 创建存储
func NewStore() *Store {
	return &Store{
		resources:    make(map[string]*types.Resource),
		downloads:    make(map[string]*types.DownloadTask),
		resourceIDs:  make([]string, 0),
		downloadIDs:  make([]string, 0),
		maxResources: 1000,
		maxDownloads: 500,
	}
}

// AddResource 添加资源
func (s *Store) AddResource(resource *types.Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查是否已存在（URL 去重）
	for _, r := range s.resources {
		if r.URL == resource.URL {
			// 更新已存在的资源
			resource.ID = r.ID
			s.resources[r.ID] = resource
			return
		}
	}

	s.resources[resource.ID] = resource
	s.resourceIDs = append(s.resourceIDs, resource.ID)

	// 超过最大数量时删除最旧的
	if len(s.resourceIDs) > s.maxResources {
		oldest := s.resourceIDs[0]
		delete(s.resources, oldest)
		s.resourceIDs = s.resourceIDs[1:]
	}
}

// GetResource 获取资源
func (s *Store) GetResource(id string) (*types.Resource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.resources[id]
	return r, ok
}

// ListResources 列出资源（支持分页和类型过滤）
func (s *Store) ListResources(resourceType types.ResourceType, page, pageSize int) ([]*types.Resource, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []*types.Resource
	// 从最新到最旧遍历
	for i := len(s.resourceIDs) - 1; i >= 0; i-- {
		r := s.resources[s.resourceIDs[i]]
		if resourceType == "" || r.Type == resourceType {
			filtered = append(filtered, r)
		}
	}

	total := len(filtered)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		return []*types.Resource{}, total
	}
	if end > total {
		end = total
	}

	return filtered[start:end], total
}

// DeleteResource 删除资源
func (s *Store) DeleteResource(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.resources[id]; !ok {
		return false
	}
	delete(s.resources, id)
	// 从 ID 列表中移除
	for i, rid := range s.resourceIDs {
		if rid == id {
			s.resourceIDs = append(s.resourceIDs[:i], s.resourceIDs[i+1:]...)
			break
		}
	}
	return true
}

// ClearResources 清空所有资源
func (s *Store) ClearResources() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources = make(map[string]*types.Resource)
	s.resourceIDs = make([]string, 0)
}

// AddDownload 添加下载任务
func (s *Store) AddDownload(task *types.DownloadTask) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.downloads[task.ID] = task
	s.downloadIDs = append(s.downloadIDs, task.ID)

	// 超过最大数量时删除最旧的已完成任务
	if len(s.downloadIDs) > s.maxDownloads {
		// 找到第一个已完成的任务删除
		for i, did := range s.downloadIDs {
			if d, ok := s.downloads[did]; ok && d.Status == types.DownloadCompleted {
				delete(s.downloads, did)
				s.downloadIDs = append(s.downloadIDs[:i], s.downloadIDs[i+1:]...)
				break
			}
		}
	}
}

// GetDownload 获取下载任务
func (s *Store) GetDownload(id string) (*types.DownloadTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.downloads[id]
	return d, ok
}

// UpdateDownload 更新下载任务
func (s *Store) UpdateDownload(task *types.DownloadTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.UpdatedAt = time.Now()
	s.downloads[task.ID] = task
}

// ListDownloads 列出下载任务
func (s *Store) ListDownloads(status types.DownloadStatus, page, pageSize int) ([]*types.DownloadTask, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []*types.DownloadTask
	for i := len(s.downloadIDs) - 1; i >= 0; i-- {
		d := s.downloads[s.downloadIDs[i]]
		if status == "" || d.Status == status {
			filtered = append(filtered, d)
		}
	}

	total := len(filtered)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		return []*types.DownloadTask{}, total
	}
	if end > total {
		end = total
	}

	return filtered[start:end], total
}

// DeleteDownload 删除下载任务
func (s *Store) DeleteDownload(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.downloads[id]; !ok {
		return false
	}
	delete(s.downloads, id)
	for i, did := range s.downloadIDs {
		if did == id {
			s.downloadIDs = append(s.downloadIDs[:i], s.downloadIDs[i+1:]...)
			break
		}
	}
	return true
}

// GetAllDownloads 获取所有下载任务（用于管理器遍历）
func (s *Store) GetAllDownloads() []*types.DownloadTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*types.DownloadTask, 0, len(s.downloads))
	for _, id := range s.downloadIDs {
		if d, ok := s.downloads[id]; ok {
			tasks = append(tasks, d)
		}
	}
	return tasks
}
