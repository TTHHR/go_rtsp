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
	"github.com/tthhr/go_rtsp/net/rtsp"
	"github.com/tthhr/go_rtsp/utils"
)

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
	streams        map[string]*StreamContext
	streamsMu      sync.RWMutex
)

//export InitRTSPServer
func InitRTSPServer(port int) {
	config := rtsp.RTSPServerInitConfig{
		Port:        port,                    //rtsp协议监听端口
		UdpEnable:   true,                    //udp传输启用？
		TcpEnable:   false,                   //tcp传输启用？网络环境较差建议启用
		ProtocolLog: true,                    //rtsp协议交互过程是否打印
		MaxClient:   2,                       //最大客户端数量
		MaxAction:   rtsp.StrategyKickOldest, //客户端满了之后的动作
		ServerName:  "THR's Server",          //rtsp协议中显示的服务端名
	}
	var err error
	serverInstance, err = api.NewServerAPI(config)
	if err != nil {
		utils.Error("config err %v", err)
	}
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
		utils.Error("!!rtsp not init!!")
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

	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()
	nalus := splitNALUs(rawBytes)

	for i, nalu := range nalus {
		isLast := (i == len(nalus)-1)
		ctx.processAndSendNALU(nalu, uint32(timestamp), isLast, false)
	}
}

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
