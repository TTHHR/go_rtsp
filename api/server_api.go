package api

import (
	"fmt"
	"sync"

	"github.com/tthhr/go_rtsp/net/rtsp"
	"github.com/tthhr/go_rtsp/utils"
)

type ServerAPI struct {
	config     rtsp.RTSPServerInitConfig
	rtspServer *rtsp.RTSPServer
	streamMgr  *StreamManager
	isRunning  bool
	mu         sync.RWMutex
	stopChan   chan struct{}
}

func NewServerAPI(config rtsp.RTSPServerInitConfig) (*ServerAPI, error) {
	server, err := rtsp.NewRTSPServer(config)
	if err != nil {
		return nil, err
	}
	streamMgr := NewStreamManager(server)

	return &ServerAPI{
		config:     config,
		rtspServer: server,
		streamMgr:  streamMgr,
		isRunning:  false,
		stopChan:   make(chan struct{}),
	}, nil
}

func (api *ServerAPI) Start() error {
	api.mu.Lock()
	defer api.mu.Unlock()

	if api.isRunning {
		return fmt.Errorf("server is already running")
	}

	// Start RTSP server
	err := api.rtspServer.Start()
	if err != nil {
		return fmt.Errorf("failed to start RTSP server: %v", err)
	}

	api.isRunning = true
	utils.Info("Server started ")

	return nil
}

func (api *ServerAPI) Stop() {
	api.mu.Lock()
	defer api.mu.Unlock()

	if !api.isRunning {
		return
	}

	close(api.stopChan)

	// Stop RTSP server
	api.rtspServer.Stop()

	api.isRunning = false
	utils.Info("Server stopped")
}

func (api *ServerAPI) PushVideoStream(path string, data []byte, timestamp uint32, marker bool) error {
	if !api.isRunning {
		return fmt.Errorf("server is not running")
	}

	return api.streamMgr.PushVideoFrame(path, data, timestamp, marker)
}

func (api *ServerAPI) AddStream(path string) {
	api.streamMgr.AddStream(path)
}

func (api *ServerAPI) RemoveStream(path string) {
	api.streamMgr.RemoveStream(path)
}

func (api *ServerAPI) GetStreams() []StreamInfo {
	return api.streamMgr.GetStreams()
}

func (api *ServerAPI) GetStreamInfo(path string) (*StreamInfo, bool) {
	return api.streamMgr.GetStreamInfo(path)
}
func (api *ServerAPI) GetSessionCount(path string) int {
	return api.streamMgr.server.GetSessionCount(path)
}
func (api *ServerAPI) GetAllSessionCount() int {
	return api.streamMgr.server.GetAllSessionCount()
}
