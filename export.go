package main

/*
#include <stdlib.h>
#include <stdint.h>
*/
import "C"

import (
	"sync"
	"time"
	"unsafe"

	"github.com/tthhr/go_rtsp/api"
	"github.com/tthhr/go_rtsp/net/rtp"
	"github.com/tthhr/go_rtsp/utils"
)

// StreamContext 用于隔离每个流的状态
type StreamContext struct {
	Path       string
	Packetizer *rtp.RTPPacketizer
	CachedVPS  []byte
	CachedSPS  []byte
	CachedPPS  []byte
	Mutex      sync.Mutex // 保护该流的内部状态
}

var (
	serverInstance *api.ServerAPI
	// streams 用于存储所有活跃的流上下文： path -> *StreamContext
	streams   map[string]*StreamContext
	streamsMu sync.RWMutex
)

//export InitRTSPServer
func InitRTSPServer(port int) {
	config := api.ServerConfig{
		RTSPPort:   port,
		BufferSize: 4 * 1024 * 1024,
		MaxClients: 100,
	}

	serverInstance = api.NewServerAPI(config)
	if err := serverInstance.Start(); err != nil {
		utils.Error("Failed to start server: %s", err.Error())
		return
	}

	// 初始化流映射表
	streams = make(map[string]*StreamContext)
	utils.Info("RTSP Server initialized on port %d", port)
}

//export AddStream
func AddStream(path *C.uchar, length C.int) {
	if serverInstance == nil {
		return
	}

	// 1. 转换 C 字符串到 Go 字符串
	goPath := C.GoStringN((*C.char)(unsafe.Pointer(path)), length)

	streamsMu.Lock()
	defer streamsMu.Unlock()

	// 2. 查重
	if _, exists := streams[goPath]; exists {
		utils.Warn("Stream already exists: %s", goPath)
		return
	}
	serverInstance.AddStream(goPath)

	ctx := &StreamContext{
		Path:       goPath,
		Packetizer: rtp.NewRTPPacketizer(96, 90000),
		// 缓存初始化为空切片
		CachedVPS: nil,
		CachedSPS: nil,
		CachedPPS: nil,
	}

	streams[goPath] = ctx
	utils.Info("Added stream: %s", goPath)
}

//export PushH265Frame
func PushH265Frame(path *C.uchar, pathlen C.int, data *C.uchar, length C.int, timestamp C.uint32_t) {
	if serverInstance == nil {
		return
	}

	// 1. 获取对应的 Stream Context
	goPath := C.GoStringN((*C.char)(unsafe.Pointer(path)), pathlen)

	streamsMu.RLock()
	ctx, exists := streams[goPath]
	streamsMu.RUnlock()

	if !exists {
		utils.Error("Stream path not found: %s", goPath)
		return
	}

	// 2. 转换数据
	rawBytes := C.GoBytes(unsafe.Pointer(data), length)
	if len(rawBytes) == 0 {
		return
	}

	// 3. 锁定该流的 Context 进行处理
	// 使用细粒度锁，避免阻塞其他流的推流
	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()

	// 执行切分 (splitNALUs 是纯函数，无需改动)
	nalus := splitNALUs(rawBytes)

	// 逐个发送，调用 Context 的方法而不是全局函数
	for i, nalu := range nalus {
		isLast := (i == len(nalus)-1)
		ctx.processAndSendNALU(nalu, uint32(timestamp), isLast, false)
	}
}

// -------------------------------------------------------------------
// StreamContext 的方法 (替代原来的全局函数)
// -------------------------------------------------------------------

func (ctx *StreamContext) processAndSendNALU(data []byte, ts uint32, isLastNALU bool, skipCompensation bool) {
	if len(data) < 2 {
		return
	}

	nalType := (data[0] >> 1) & 0x3F

	// 更新 Context 内部的缓存
	switch nalType {
	case 32: // VPS
		ctx.CachedVPS = make([]byte, len(data))
		copy(ctx.CachedVPS, data)
	case 33: // SPS
		ctx.CachedSPS = make([]byte, len(data))
		copy(ctx.CachedSPS, data)
	case 34: // PPS
		ctx.CachedPPS = make([]byte, len(data))
		copy(ctx.CachedPPS, data)
	}

	// 关键帧补偿逻辑 (使用 Context 内的缓存)
	if (nalType >= 19 && nalType <= 21) && !skipCompensation {
		if len(ctx.CachedVPS) > 0 {
			ctx.sendInternal(ctx.CachedVPS, ts, false)
		}
		if len(ctx.CachedSPS) > 0 {
			ctx.sendInternal(ctx.CachedSPS, ts, false)
		}
		if len(ctx.CachedPPS) > 0 {
			ctx.sendInternal(ctx.CachedPPS, ts, false)
		}
	}

	ctx.sendInternal(data, ts, isLastNALU)
}

func (ctx *StreamContext) sendInternal(data []byte, ts uint32, useMarker bool) {
	// 使用 Context 自己的打包器
	packets := ctx.Packetizer.PacketizeH265NALU(data, ts)

	// 拼接完整的 RTP 路径，例如 "test/streamid=0"
	// 注意：这里的 Path 应该是 cleanPath，无需手动加斜杠，除非你的 Server 逻辑特殊
	fullPath := ctx.Path

	if !useMarker && len(packets) > 0 {
		lastIdx := len(packets) - 1
		packets[lastIdx][1] &= 0x7F
	}

	for i, pkt := range packets {
		isMarkerPacket := (i == len(packets)-1) && useMarker

		// 调用 ServerAPI 推流
		serverInstance.PushVideoStream(fullPath, pkt, ts, isMarkerPacket)

		// 简单的 Pacing
		time.Sleep(1 * time.Microsecond)
	}
}

// -------------------------------------------------------------------
// 工具函数 (保持不变)
// -------------------------------------------------------------------

func splitNALUs(data []byte) [][]byte {
	var nalus [][]byte
	if len(data) == 0 {
		return nalus
	}

	start := 0
	// 从第 1 个字节开始扫，寻找 00 00 01
	for i := 0; i < len(data)-3; i++ {
		// 优化：先判断 data[i] 是否为 0
		if data[i] != 0 {
			continue
		}

		// 命中 00 00 01
		if data[i+1] == 0 && data[i+2] == 1 {
			if i > start {
				nalus = append(nalus, data[start:i])
			}
			start = i + 3
			i += 2
			continue
		}

		// 命中 00 00 00 01
		if data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			if i > start {
				nalus = append(nalus, data[start:i])
			}
			start = i + 4
			i += 3
			continue
		}
	}

	// 收尾
	if start < len(data) {
		nalus = append(nalus, data[start:])
	}

	return nalus
}

//export StopRTSPServer
func StopRTSPServer() {
	if serverInstance != nil {
		serverInstance.Stop()
	}
}

func main() {}
