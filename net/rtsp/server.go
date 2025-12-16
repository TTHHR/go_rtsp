package rtsp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/tthhr/go_rtsp/net/transport"
	"github.com/tthhr/go_rtsp/utils"
)

type LimitStrategy int

const (
	StrategyReject     LimitStrategy = iota // 0: 直接拒绝
	StrategyKickOldest                      // 1: 踢出最旧的
	StrategyIgnore                          // 2: 忽略限制（强行加入）
)

type RTSPServerInitConfig struct {
	Port        int
	ProtocolLog bool
	TcpEnable   bool
	UdpEnable   bool
	ServerName  string
	MaxClient   int           //最大客户端数量
	MaxAction   LimitStrategy //客户端满了之后的动作
}

type RTSPServer struct {
	availablePaths map[string]string
	address        string
	protocolLog    bool
	tcpEnable      bool
	udpEnable      bool
	serverName     string
	maxClient      int           //最大客户端数量
	maxAction      LimitStrategy //客户端满了之后的动作
	tcpServer      *transport.TCPServer
	sessions       map[string]*StreamSession
	sessionCounts  map[string]int
	mu             sync.RWMutex
	nextCSeq       int
}

func (s *RTSPServer) AddPath(path string) {
	s.availablePaths[path] = path
}
func (s *RTSPServer) RemovePath(path string) {
	s.availablePaths[path] = ""
}
func (s *RTSPServer) GetSessionCount(path string) int {
	var count = 0
	for _, session := range s.sessions {
		if strings.HasPrefix(session.StreamPath, path) {
			count++
		}
	}
	return count
}
func (s *RTSPServer) GetAllSessionCount() int {
	return len(s.sessions)
}

func NewRTSPServer(config RTSPServerInitConfig) (*RTSPServer, error) {
	if !config.UdpEnable && !config.TcpEnable {
		return nil, fmt.Errorf("err tcp & udp all disable")
	}
	return &RTSPServer{
		availablePaths: make(map[string]string),
		address:        fmt.Sprintf(":%d", config.Port),
		protocolLog:    config.ProtocolLog,
		tcpEnable:      config.TcpEnable,
		udpEnable:      config.UdpEnable,
		serverName:     config.ServerName,
		maxClient:      config.MaxClient,
		maxAction:      config.MaxAction,
		sessions:       make(map[string]*StreamSession),
		sessionCounts:  make(map[string]int),
		nextCSeq:       1,
	}, nil
}

func (s *RTSPServer) Start() error {
	tcpServer, err := transport.NewTCPServer(s.address)
	if err != nil {
		return err
	}

	s.tcpServer = tcpServer
	s.tcpServer.Handler = s.handleRTSPConnection

	go s.tcpServer.Start()
	utils.Info("RTSP server started on %s", s.address)

	return nil
}

func (s *RTSPServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close all sessions
	for _, session := range s.sessions {
		session.Close()
	}
	s.sessions = make(map[string]*StreamSession)

	// Stop TCP server
	if s.tcpServer != nil {
		s.tcpServer.Stop()
	}

	utils.Info("RTSP server stopped")
}

func (s *RTSPServer) handleRTSPConnection(conn net.Conn) {
	defer conn.Close()

	clientAddr := conn.RemoteAddr().String()
	utils.Info("New RTSP connection from %s", clientAddr)

	reader := bufio.NewReader(conn)
	var currentSession *StreamSession

	defer func() {
		if currentSession != nil {
			utils.Info("Connection closed, cleaning up session: %s", currentSession.SessionID)
			// 关闭 Session 内部资源（如 UDP 连接）
			currentSession.Close()
			// 从全局 Map 中移除
			s.removeSession(currentSession.SessionID)
		}
	}()

	for {
		// Read RTSP request
		var requestBuilder strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				utils.Error("Read error: %s", err.Error())
				return
			}

			requestBuilder.WriteString(line)
			if line == "\r\n" {
				break
			}
		}

		requestData := requestBuilder.String()
		if s.protocolLog {
			utils.Debug("Received request:\n%s", requestData)
		}

		// Parse request
		req := ParseRTSPRequest(requestData)
		if req == nil {
			utils.Error("Failed to parse RTSP request")
			return
		}

		// Get CSeq
		cseq := 0
		if cseqStr, ok := req.Headers["CSeq"]; ok {
			fmt.Sscanf(cseqStr, "%d", &cseq)
		}

		// Handle different methods
		var response string
		switch req.Method {
		case MethodOptions:
			response = s.handleOptions(req, cseq)
		case MethodDescribe:
			response = s.handleDescribe(req, cseq)
		case MethodSetup:
			resp, session := s.handleSetup(req, cseq, conn)
			response = resp
			if session != nil {
				currentSession = session
				utils.Info("session id %s", session.SessionID)
			}
		case MethodPlay:
			response = s.handlePlay(req, cseq, currentSession)
		case MethodTeardown:
			response = s.handleTeardown(req, cseq, currentSession)
		case MethodAnnounce:
			response = s.handleAnnounce(req, cseq, requestData)
		case MethodRecord:
			response = s.handleRecord(req, cseq, currentSession)
		default:
			response = BuildRTSPResponse(405, "Method Not Allowed", map[string]string{
				"CSeq": fmt.Sprintf("%d", cseq),
			}, "")
		}

		// Send response
		if s.protocolLog {
			utils.Debug("Sending response:\n%s", response)
		}

		conn.Write([]byte(response))
	}
}

func (s *RTSPServer) handleOptions(req *RTSPRequest, cseq int) string {
	headers := map[string]string{
		"CSeq":   fmt.Sprintf("%d", cseq),
		"Public": "OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY, ANNOUNCE, RECORD",
		"Server": s.serverName,
	}
	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleDescribe(req *RTSPRequest, cseq int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Extract stream path from URL
	streamPath := extractStreamPath(req.URL)
	path, ok := s.availablePaths[streamPath]
	if !ok || path == "" {
		utils.Warn("Stream not found: %s", streamPath)
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": s.serverName,
		}
		return BuildRTSPResponse(404, "Not Found", headers, "")
	}
	if s.sessionCounts[streamPath] >= s.maxClient {
		utils.Warn("Stream max: %s", streamPath)
		switch s.maxAction {
		case StrategyReject:
			headers := map[string]string{
				"CSeq":   fmt.Sprintf("%d", cseq),
				"Server": s.serverName,
			}
			return BuildRTSPResponse(404, "Not Found", headers, "")
		case StrategyIgnore:
			utils.Info("allow client enter")
		case StrategyKickOldest:
			session, found := s.GetOldestSessionByPath(streamPath)
			if found && session != nil {
				session.NeedClose = true
			}
		}

	}

	// Create a temporary session for SDP generation
	tempSession := NewStreamSession(streamPath)
	tempSession.SetupTransport("RTP/AVP/UDP", nil)
	utils.Debug("create new seesion %s for %s", tempSession.SessionID, tempSession.StreamPath)

	sdp := tempSession.GetSDP()
	s.sessions[tempSession.SessionID] = tempSession
	s.sessionCounts[streamPath]++

	headers := map[string]string{
		"CSeq":         fmt.Sprintf("%d", cseq),
		"Content-Base": req.URL + "/",
		"Content-Type": "application/sdp",
		"Server":       s.serverName,
	}

	return BuildRTSPResponse(200, "OK", headers, sdp)
}

func (s *RTSPServer) handleSetup(req *RTSPRequest, cseq int, conn net.Conn) (string, *StreamSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID := req.Session

	var session *StreamSession
	var ok bool

	// Use existing session
	session, ok = s.sessions[sessionID]
	if !ok {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": s.serverName,
		}
		return BuildRTSPResponse(454, "Session Not Found", headers, ""), nil
	}

	// Parse transport
	transport := req.Transport

	// Parse client ports from transport header
	mode, tcpOrUdp, clientRTPPort, clientRTCPPort, _, _, _, err := utils.ParseTransport(transport)
	if err != nil {
		utils.Error("ParseTransport fail %s", err.Error())
	}
	if (tcpOrUdp && !s.tcpEnable) || (!tcpOrUdp && !s.udpEnable) {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": s.serverName,
		}
		return BuildRTSPResponse(405, "Method Not Support", headers, ""), nil
	}
	session.isTcp = tcpOrUdp
	// Setup transport
	var clientAddr *net.UDPAddr
	if mode == "unicast" {
		// For UDP, client will send packets to our server
		clientIP := strings.Split(conn.RemoteAddr().String(), ":")[0]
		utils.Debug("new connect %s", clientIP)
		clientAddr = &net.UDPAddr{
			IP:   net.ParseIP(clientIP),
			Port: clientRTPPort,
		}
		session.ClientRTPPort = clientRTPPort
		session.ClientRTCPort = clientRTCPPort
	}

	err = session.SetupTransport(transport, clientAddr)
	if err != nil {
		utils.Error("SetupTransport error: %s", err.Error())
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": s.serverName,
		}
		return BuildRTSPResponse(500, "Internal Server Error", headers, ""), nil
	}

	// Prepare transport response
	var transportResponse string
	if session.isTcp {
		transportResponse = "RTP/AVP/TCP;interleaved=0-1"
	} else {
		transportResponse = fmt.Sprintf("RTP/AVP/UDP;unicast;client_port=%d-%d;server_port=%d-%d",
			clientRTPPort, clientRTCPPort, session.ServerRTPPort, session.ServerRTCPPort)
	}

	headers := map[string]string{
		"CSeq":      fmt.Sprintf("%d", cseq),
		"Session":   sessionID + ";timeout=60",
		"Transport": transportResponse,
		"Server":    s.serverName,
	}

	session.State = "ready"
	session.RTSPConn = conn

	return BuildRTSPResponse(200, "OK", headers, ""), session
}

func (s *RTSPServer) handlePlay(req *RTSPRequest, cseq int, session *StreamSession) string {
	if session == nil {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": s.serverName,
		}
		return BuildRTSPResponse(454, "Session Not Found", headers, "")
	}

	session.State = "playing"
	session.UpdateActivity()

	headers := map[string]string{
		"CSeq":    fmt.Sprintf("%d", cseq),
		"Session": session.SessionID,
		"Range":   "npt=0.000-",
		//"RTP-Info": fmt.Sprintf("url=%s;seq=1", req.URL),
		"Server": s.serverName,
	}

	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleTeardown(req *RTSPRequest, cseq int, session *StreamSession) string {
	if session != nil {
		session.Close()
		s.removeSession(session.SessionID)
	}

	headers := map[string]string{
		"CSeq":    fmt.Sprintf("%d", cseq),
		"Session": req.Session,
		"Server":  s.serverName,
	}

	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleAnnounce(req *RTSPRequest, cseq int, body string) string {
	// Announce is used to push SDP to server
	headers := map[string]string{
		"CSeq":   fmt.Sprintf("%d", cseq),
		"Server": s.serverName,
	}
	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleRecord(req *RTSPRequest, cseq int, session *StreamSession) string {
	if session == nil {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": s.serverName,
		}
		return BuildRTSPResponse(454, "Session Not Found", headers, "")
	}

	session.State = "recording"
	session.UpdateActivity()

	headers := map[string]string{
		"CSeq":    fmt.Sprintf("%d", cseq),
		"Session": session.SessionID,
		"Server":  s.serverName,
	}

	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) removeSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionCounts[s.sessions[sessionID].StreamPath]--
	delete(s.sessions, sessionID)
}

func (s *RTSPServer) PushVideoFrame(streamPath string, data []byte, timestamp uint32, marker bool) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find all sessions for this stream path
	for _, session := range s.sessions {
		if strings.HasPrefix(session.StreamPath, streamPath) && session.State == "playing" {
			//go session.SendRTPPacket(data, timestamp, marker)
			session.SendRTPPacket(data, timestamp, marker)
			if session.NeedClose && session.RTSPConn != nil {
				utils.Info("session %s close", session.SessionID)
				session.RTSPConn.Close()
			}
		}
	}

	return nil
}

func extractStreamPath(url string) string {
	// Remove protocol and host
	if idx := strings.Index(url, "://"); idx > 0 {
		url = url[idx+3:]
		if slashIdx := strings.Index(url, "/") + 1; slashIdx > 0 {
			url = url[slashIdx:]
		}
	}
	return url
}
