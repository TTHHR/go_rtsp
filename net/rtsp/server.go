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

type RTSPServer struct {
	availablePaths map[string]string
	address        string
	tcpServer      *transport.TCPServer
	sessions       map[string]*StreamSession
	sessionByCSeq  map[int]string
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

func NewRTSPServer(addr string) *RTSPServer {
	return &RTSPServer{
		availablePaths: make(map[string]string),
		address:        addr,
		sessions:       make(map[string]*StreamSession),
		sessionByCSeq:  make(map[int]string),
		nextCSeq:       1,
	}
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
		//utils.Debug("Received request:\n%s", requestData)

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
		//utils.Debug("Sending response:\n%s", response)
		conn.Write([]byte(response))
	}
}

func (s *RTSPServer) handleOptions(req *RTSPRequest, cseq int) string {
	headers := map[string]string{
		"CSeq":   fmt.Sprintf("%d", cseq),
		"Public": "OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY, ANNOUNCE, RECORD",
		"Server": "Go-RTSP-Server",
	}
	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleDescribe(req *RTSPRequest, cseq int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Extract stream path from URL
	streamPath := extractStreamPath(req.URL)
	path, ok := s.availablePaths[streamPath]
	if !ok || path == "" {
		utils.Warn("Stream not found: %s", streamPath)
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": "Go-RTSP-Server",
		}
		return BuildRTSPResponse(404, "Not Found", headers, "")
	}

	// Create a temporary session for SDP generation
	tempSession := NewStreamSession(streamPath)
	tempSession.SetupTransport("RTP/AVP/UDP", nil)
	utils.Debug("create new seesion %s for %s", tempSession.SessionID, tempSession.StreamPath)

	sdp := tempSession.GetSDP()
	s.sessions[tempSession.SessionID] = tempSession

	headers := map[string]string{
		"CSeq":         fmt.Sprintf("%d", cseq),
		"Content-Base": req.URL + "/",
		"Content-Type": "application/sdp",
		// "Content-Length": fmt.Sprintf("%d", len(sdp)),
		"Server": "Go-RTSP-Server",
	}

	return BuildRTSPResponse(200, "OK", headers, sdp)
}

func (s *RTSPServer) handleSetup(req *RTSPRequest, cseq int, conn net.Conn) (string, *StreamSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID := req.Session

	var session *StreamSession
	var ok bool

	utils.Debug("setup sessionid %s", sessionID)

	// Use existing session
	session, ok = s.sessions[sessionID]
	if !ok {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": "Go-RTSP-Server",
		}
		return BuildRTSPResponse(454, "Session Not Found", headers, ""), nil
	}

	// Parse transport
	transport := req.Transport
	if transport == "" {
		transport = "RTP/AVP/UDP"
	}

	// Parse client ports from transport header
	mode, tcpOrUdp, clientRTPPort, clientRTCPPort, _, _, _, err := utils.ParseTransport(transport)
	if err != nil {
		utils.Error("ParseTransport fail %s", err.Error())
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
			"Server": "Go-RTSP-Server",
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
		"Server":    "Go-RTSP-Server",
	}

	session.State = "ready"
	session.RTSPConn = conn

	return BuildRTSPResponse(200, "OK", headers, ""), session
}

func (s *RTSPServer) handlePlay(req *RTSPRequest, cseq int, session *StreamSession) string {
	if session == nil {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": "Go-RTSP-Server",
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
		"Server": "Go-RTSP-Server",
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
		"Server":  "Go-RTSP-Server",
	}

	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleAnnounce(req *RTSPRequest, cseq int, body string) string {
	// Announce is used to push SDP to server
	headers := map[string]string{
		"CSeq":   fmt.Sprintf("%d", cseq),
		"Server": "Go-RTSP-Server",
	}
	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) handleRecord(req *RTSPRequest, cseq int, session *StreamSession) string {
	if session == nil {
		headers := map[string]string{
			"CSeq":   fmt.Sprintf("%d", cseq),
			"Server": "Go-RTSP-Server",
		}
		return BuildRTSPResponse(454, "Session Not Found", headers, "")
	}

	session.State = "recording"
	session.UpdateActivity()

	headers := map[string]string{
		"CSeq":    fmt.Sprintf("%d", cseq),
		"Session": session.SessionID,
		"Server":  "Go-RTSP-Server",
	}

	return BuildRTSPResponse(200, "OK", headers, "")
}

func (s *RTSPServer) removeSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
	// Also remove from CSeq map
	for cseq, sid := range s.sessionByCSeq {
		if sid == sessionID {
			delete(s.sessionByCSeq, cseq)
		}
	}
}

func (s *RTSPServer) PushVideoFrame(streamPath string, data []byte, timestamp uint32, marker bool) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find all sessions for this stream path
	for _, session := range s.sessions {
		if strings.HasPrefix(session.StreamPath, streamPath) && session.State == "playing" {
			//go session.SendRTPPacket(data, timestamp, marker)
			session.SendRTPPacket(data, timestamp, marker)
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
