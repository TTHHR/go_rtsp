package api

import (
	"fmt"
	"sync"
	"time"

	"github.com/tthhr/go_rtsp/net/rtsp"
	"github.com/tthhr/go_rtsp/utils"
)

type StreamInfo struct {
	Path        string
	CreatedAt   time.Time
	lastFrameAt time.Time
}

type StreamManager struct {
	server  *rtsp.RTSPServer
	streams map[string]*StreamInfo
	mu      sync.RWMutex
}

func NewStreamManager(server *rtsp.RTSPServer) *StreamManager {
	return &StreamManager{
		server:  server,
		streams: make(map[string]*StreamInfo),
	}
}

func (m *StreamManager) AddStream(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.streams[path]; !exists {
		m.streams[path] = &StreamInfo{
			Path:        path,
			CreatedAt:   time.Now(),
			lastFrameAt: time.Now(),
		}
		m.server.AddPath(path)
		utils.Info("Stream added: %s", path)
	}
}

func (m *StreamManager) RemoveStream(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.streams, path)
	m.server.RemovePath(path)
	utils.Info("Stream remove: %s", path)
}

func (m *StreamManager) PushVideoFrame(path string, data []byte, timestamp uint32, marker bool) error {
	m.mu.Lock()
	// Update stream info
	_, exists := m.streams[path]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("target path not exist")
	}

	err := m.server.PushVideoFrame(path, data, timestamp, marker)
	return err
}

func (m *StreamManager) GetStreams() []StreamInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	streams := make([]StreamInfo, 0, len(m.streams))
	for _, info := range m.streams {
		streams = append(streams, *info)
	}
	return streams
}

func (m *StreamManager) GetStreamInfo(path string) (*StreamInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, exists := m.streams[path]
	if exists {
		copy := *info
		return &copy, true
	}
	return nil, false
}
