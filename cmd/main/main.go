package main

import (
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tthhr/go_rtsp/api"
	"github.com/tthhr/go_rtsp/net/rtp"
	"github.com/tthhr/go_rtsp/net/rtsp"
	"github.com/tthhr/go_rtsp/utils"
)

func main() {
	// Parse command line arguments
	rtspPort := flag.Int("rtsp-port", 8554, "RTSP server port")
	filePath := flag.String("h265-file", "", "h265 file path")
	flag.Parse()
	// Create server configuration
	config := rtsp.RTSPServerInitConfig{
		Port:        *rtspPort,               //rtsp协议监听端口
		UdpEnable:   true,                    //udp传输启用？
		TcpEnable:   false,                   //tcp传输启用？网络环境较差建议启用
		ProtocolLog: true,                    //rtsp协议交互过程是否打印
		MaxClient:   1,                       //最大客户端数量
		MaxAction:   rtsp.StrategyKickOldest, //客户端满了之后的动作
		ServerName:  "THR's Server",          //rtsp协议中显示的服务端名
	}

	// Create and start server
	server, err := api.NewServerAPI(config)
	if err != nil {
		utils.Error("config err ,%v", err)
		os.Exit(1)
	}

	if err := server.Start(); err != nil {
		utils.Error("Failed to start server:%s", err.Error())
		os.Exit(1)
	}

	if *filePath != "" {
		server.AddStream("filetest")
		go simulateVideoFileStream(server, "filetest", *filePath)
	}
	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	utils.Info("Shutting down server...")

	// Stop server
	server.Stop()

	utils.Info("Server stopped")
}

func simulateVideoFileStream(server *api.ServerAPI, path string, filepath string) {
	reader, err := utils.NewH265FileReader(filepath)
	if err != nil {
		utils.Error("Failed to open file: %s", err.Error())
		return
	}
	defer reader.Stop()
	reader.Start()

	packetizer := rtp.NewRTPPacketizer(96, 90000)
	frameInterval := time.Second / 25
	timestampIncrement := uint32(90000 / 25)
	var timestamp uint32

	// --- 缓存参数集 ---
	var vps, sps, pps []byte

	for {
		nalu, err := reader.ReadNextNALU()
		if err != nil {
			if err == io.EOF {
				reader.Reset()
				continue
			}
			continue
		}

		// --- 解析 NALU 类型 (H.265) ---
		// NALU Header 是 2字节。Type 在第一个字节的 (bit 1-6)
		// (nalu.Data[0] >> 1) & 0x3F

		nalType := (nalu.Data[0] >> 1) & 0x3F
		if len(vps) == 0 || len(sps) == 0 || len(pps) == 0 {
			// 缓存关键参数
			switch nalType {
			case 32: // VPS
				vps = make([]byte, len(nalu.Data))
				copy(vps, nalu.Data)
				utils.Debug("Got VPS")
			case 33: // SPS
				sps = make([]byte, len(nalu.Data))
				copy(sps, nalu.Data)
				utils.Debug("Got SPS")
			case 34: // PPS
				pps = make([]byte, len(nalu.Data))
				copy(pps, nalu.Data)
				utils.Debug("Got PPS")
			}
		}

		isKeyFrame := (nalType >= 19 && nalType <= 21)

		if isKeyFrame {
			// 按顺序发送参数集
			if len(vps) > 0 {
				sendNalu(server, packetizer, path, vps, timestamp)
			}
			if len(sps) > 0 {
				sendNalu(server, packetizer, path, sps, timestamp)
			}
			if len(pps) > 0 {
				sendNalu(server, packetizer, path, pps, timestamp)
			}
		}

		// 发送当前帧
		sendNalu(server, packetizer, path, nalu.Data, timestamp)

		timestamp += timestampIncrement
		time.Sleep(frameInterval)
	}
}

func sendNalu(server *api.ServerAPI, packetizer *rtp.RTPPacketizer, path string, data []byte, timestamp uint32) {
	packets := packetizer.PacketizeH265NALU(data, timestamp)
	for i, pkt := range packets {
		server.PushVideoStream(path, pkt, timestamp, i == len(packets)-1)
		// 稍微加一点点间隔防止UDP发太快爆缓冲区
		time.Sleep(1 * time.Millisecond)
	}
}
