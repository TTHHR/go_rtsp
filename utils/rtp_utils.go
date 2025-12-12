package utils

import (
	"crypto/rand"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func GenerateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func ParseTransport(transport string) (string, bool, int, int, int, int, string, error) {
	// transport: RTP/AVP/UDP;unicast;client_port=3456-3457;server_port=5000-5001;mode=PLAY

	params := make(map[string]string)
	parts := strings.Split(transport, ";")
	tcpMode := false
	if strings.Contains(transport, "RTP/AVP/TCP") {
		tcpMode = true
	}
	// 解析模式和参数
	mode := "unicast" // 默认
	for _, part := range parts[1:] {
		if part == "unicast" || part == "multicast" {
			mode = part
		} else if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}

	// 解析客户端端口
	clientRtpPort := 0
	clientRtcpPort := 0
	if ports, ok := params["client_port"]; ok {
		portParts := strings.Split(ports, "-")
		if len(portParts) >= 1 {
			clientRtpPort, _ = strconv.Atoi(portParts[0])
		}
		if len(portParts) >= 2 {
			clientRtcpPort, _ = strconv.Atoi(portParts[1])
		}
	}

	// 解析服务器端口
	serverRtpPort := 0
	serverRtcpPort := 0
	if ports, ok := params["server_port"]; ok {
		portParts := strings.Split(ports, "-")
		if len(portParts) >= 1 {
			serverRtpPort, _ = strconv.Atoi(portParts[0])
		}
		if len(portParts) >= 2 {
			serverRtcpPort, _ = strconv.Atoi(portParts[1])
		}
	}

	// 会话
	session := ""
	if s, ok := params["session"]; ok {
		session = s
	}

	return mode, tcpMode, clientRtpPort, clientRtcpPort, serverRtpPort, serverRtcpPort, session, nil
}

func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}
