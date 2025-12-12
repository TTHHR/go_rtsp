package rtp

import (
	"encoding/binary"
)

// RTPPacketizer H.265 RTP打包器
type RTPPacketizer struct {
	sequenceNumber uint16
	ssrc           uint32
	payloadType    uint8
	mtuSize        int
}

// NewRTPPacketizer 创建RTP打包器
func NewRTPPacketizer(payloadType uint8, clockRate uint32) *RTPPacketizer {
	return &RTPPacketizer{
		sequenceNumber: 1,
		ssrc:           0x12345678,
		payloadType:    payloadType,
		mtuSize:        1400, // 建议设置为 1400 或 1200，留出 TCP/IP 头空间
	}
}

// PacketizeH265NALU 将 H.265 NALU 打包成 RTP 包
func (p *RTPPacketizer) PacketizeH265NALU(nalu []byte, timestamp uint32) [][]byte {
	var packets [][]byte
	naluSize := len(nalu)
	if naluSize < 2 {
		return packets
	}

	// H.265 NALU Header 是 2 字节
	// Type 在第一个字节的 (bit 1-6)
	nalType := (nalu[0] >> 1) & 0x3F

	// 减去 RTP Header(12)
	maxSinglePayload := p.mtuSize - 12

	if naluSize <= maxSinglePayload {
		// --- 单包模式 (Single NAL Unit) ---
		// 直接拷贝整个 NALU (包含头部)
		packet := p.createSinglePacket(nalu, timestamp)
		packets = append(packets, packet)
	} else {
		// --- 分片模式 (Fragmentation Unit) ---
		// H.265 分片机制 (RFC 7798 Section 4.4.3)
		packets = append(packets, p.createFUPackets(nalu, timestamp, nalType)...)
	}
	return packets
}

// 单包 RTP
func (p *RTPPacketizer) createSinglePacket(nalu []byte, timestamp uint32) []byte {
	rtpHeader := make([]byte, 12)
	rtpHeader[0] = 0x80
	rtpHeader[1] = p.payloadType & 0x7F

	// 注意：单包模式下，我们默认置 1。
	// 但在 export.go 的 sendInternal 里，我们会根据是否是这一帧的最后一个 NALU 来手动修改它。
	rtpHeader[1] |= 0x80

	binary.BigEndian.PutUint16(rtpHeader[2:4], p.sequenceNumber)
	binary.BigEndian.PutUint32(rtpHeader[4:8], timestamp)
	binary.BigEndian.PutUint32(rtpHeader[8:12], p.ssrc)

	p.sequenceNumber++

	return append(rtpHeader, nalu...)
}

// [核心修复] FU 分片 RTP (H.265 专用逻辑)
// H.264 的分片头是 2 字节，但 H.265 是 3 字节！
func (p *RTPPacketizer) createFUPackets(nalu []byte, timestamp uint32, nalType uint8) [][]byte {
	var packets [][]byte

	// 去掉原始 NALU 的 2 字节头
	naluPayload := nalu[2:]

	// 计算 Payload 空间
	// MTU - RTP头(12) - PayloadHeader(2) - FUHeader(1) = MTU - 15
	maxPayload := p.mtuSize - 12 - 3

	offset := 0
	payloadLen := len(naluPayload)

	// 总分片数 (用于计算 Marker)
	// totalFragments := int(math.Ceil(float64(payloadLen) / float64(maxPayload)))

	for offset < payloadLen {
		chunkSize := maxPayload
		if offset+chunkSize > payloadLen {
			chunkSize = payloadLen - offset
		}

		rtpHeader := make([]byte, 12)
		rtpHeader[0] = 0x80
		rtpHeader[1] = p.payloadType & 0x7F

		// 如果是最后一个分片，设置 Marker 位
		// 注意：这只是针对这个 NALU 的最后一个分片。
		// 如果这个 NALU 本身不是一帧的最后一个（比如是 VPS），
		// export.go 里的逻辑会再次把这个位抹去，这是安全的。
		if offset+chunkSize == payloadLen {
			rtpHeader[1] |= 0x80
		}

		binary.BigEndian.PutUint16(rtpHeader[2:4], p.sequenceNumber)
		binary.BigEndian.PutUint32(rtpHeader[4:8], timestamp)
		binary.BigEndian.PutUint32(rtpHeader[8:12], p.ssrc)

		// --- 构建 H.265 FU 头部 (3 字节结构) ---

		// 1. Payload Header [Byte 1]: F(1) + Type(6) + LayerIdH(1)
		// 必须将 Type 设置为 49 (FU)
		// nalu[0] & 0x81 保留了 F 位和 LayerId 的最高位
		// (49 << 1) 设置 Type 为 49 (Fragmentation Unit)
		ph1 := (nalu[0] & 0x81) | (49 << 1)

		// 2. Payload Header [Byte 2]: LayerIdL(4) + TID(3)
		// 直接拷贝原始 NALU 第2字节
		ph2 := nalu[1]

		// 3. FU Header [Byte 3]: S(1) + E(1) + FuType(6)
		var fuHeaderByte byte
		if offset == 0 {
			// Start bit
			fuHeaderByte = 0x80 | nalType
		} else if offset+chunkSize == payloadLen {
			// End bit
			fuHeaderByte = 0x40 | nalType
		} else {
			// Middle bit
			fuHeaderByte = nalType
		}

		// 组装：RTP Header + PH1 + PH2 + FU Header + Data
		packet := append(rtpHeader, ph1, ph2, fuHeaderByte)
		packet = append(packet, naluPayload[offset:offset+chunkSize]...)

		packets = append(packets, packet)

		offset += chunkSize
		p.sequenceNumber++
	}

	return packets
}
