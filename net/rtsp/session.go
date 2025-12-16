package rtsp

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/tthhr/go_rtsp/net/rtp"
	"github.com/tthhr/go_rtsp/net/transport"
	"github.com/tthhr/go_rtsp/utils"
)

type StreamSession struct {
	SessionID      string
	StreamPath     string
	ClientRTPPort  int
	ClientRTCPort  int
	ServerRTPPort  int
	ServerRTCPPort int
	ClientAddr     *net.UDPAddr
	State          string
	Transport      string // "RTP/AVP/UDP" or "RTP/AVP/TCP"
	isTcp          bool

	// For RTP over TCP
	RTSPConn    net.Conn
	Interleaved bool
	RTPChannel  int
	RTCPChannel int

	// For RTP over UDP
	UDPServerRTP  *transport.UDPServer
	UDPServerRTCP *transport.UDPServer
	RTPSender     *rtp.RTPSender

	LastActive time.Time
	NeedClose  bool
	Sequence   uint16
	mu         sync.RWMutex
}

func NewStreamSession(streamPath string) *StreamSession {
	sessionID := utils.GenerateSessionID()

	return &StreamSession{
		SessionID:  sessionID,
		StreamPath: streamPath,
		State:      "init",
		Transport:  "RTP/AVP/UDP",
		Sequence:   1,
		LastActive: time.Now(),
		isTcp:      false,
	}
}

func (s *StreamSession) SetupTransport(transport string, clientAddr *net.UDPAddr) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Transport = transport
	s.ClientAddr = clientAddr

	if s.isTcp {
		return s.setupTCPTransport()
	} else {
		return s.setupUDPTransport()
	}
}

func (s *StreamSession) setupUDPTransport() error {
	// Find available ports for RTP
	rtpPort, err := transport.FindAvailableUDPPort(30000)
	if err != nil {
		return err
	}

	// Find available port for RTCP
	rtcpPort, err := transport.FindAvailableUDPPort(rtpPort + 1)
	if err != nil {
		return err
	}

	// Create UDP servers
	rtpServer, err := transport.NewUDPServer(rtpPort)
	if err != nil {
		return err
	}

	rtcpServer, err := transport.NewUDPServer(rtcpPort)
	if err != nil {
		rtpServer.Close()
		return err
	}

	s.UDPServerRTP = rtpServer
	s.UDPServerRTCP = rtcpServer
	s.ServerRTPPort = rtpPort
	s.ServerRTCPPort = rtcpPort

	// Create RTP sender
	s.RTPSender = rtp.NewRTPSender(rtpServer.Conn())

	utils.Info("UDP transport setup: RTP port=%d, RTCP port=%d", rtpPort, rtcpPort)
	return nil
}

func (s *StreamSession) setupTCPTransport() error {
	// For TCP interleaved, channels are set during SETUP
	s.Interleaved = true
	s.RTPChannel = 0
	s.RTCPChannel = 1
	utils.Info("TCP interleaved transport setup")
	return nil
}

func (s *StreamSession) SendRTPPacket(data []byte, timestamp uint32, marker bool) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.State != "playing" {
		return fmt.Errorf("session not in playing state")
	}

	if s.isTcp && s.RTSPConn != nil {
		interleavedData := s.buildInterleavedPacket(data, s.RTCPChannel)
		_, err := s.RTSPConn.Write(interleavedData)
		return err
	} else if s.RTPSender != nil && s.ClientAddr != nil {
		return s.RTPSender.SendRawData(data, s.ClientAddr)
	}

	return fmt.Errorf("no valid transport for sending RTP")
}

func (s *StreamSession) buildInterleavedPacket(rtpData []byte, channel int) []byte {
	packet := make([]byte, len(rtpData)+4)
	packet[0] = '$'
	packet[1] = byte(channel)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(rtpData)))
	copy(packet[4:], rtpData)
	return packet
}

func (s *StreamSession) UpdateActivity() {
	s.mu.Lock()
	s.LastActive = time.Now()
	s.mu.Unlock()
}

func (s *StreamSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.UDPServerRTP != nil {
		s.UDPServerRTP.Close()
	}
	if s.UDPServerRTCP != nil {
		s.UDPServerRTCP.Close()
	}
	if s.RTPSender != nil {
		s.RTPSender.Close()
	}

	s.State = "closed"
	utils.Info("Session closed")
}

func (s *StreamSession) GetSDP() string {
	// Generate SDP for H.264 video
	sdp := fmt.Sprintf(`v=0
o=- 0 0 IN IP4 0.0.0.0
s=H265 Video Stream
c=IN IP4 0.0.0.0
t=0 0
m=video %d RTP/AVP 96
a=rtpmap:96 H265/90000
a=control:streamid=%s
`, s.ServerRTPPort, s.SessionID)

	return sdp
}

func (s *RTSPServer) GetOldestSessionByPath(streamPath string) (*StreamSession, bool) {

	var oldestSession *StreamSession
	var oldestTime time.Time
	found := false

	for _, session := range s.sessions {
		if session.StreamPath == streamPath {
			if !found || session.LastActive.Before(oldestTime) {
				oldestSession = session
				oldestTime = session.LastActive
				found = true
			}
		}
	}

	return oldestSession, found
}
