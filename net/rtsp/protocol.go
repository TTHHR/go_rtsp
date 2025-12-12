package rtsp

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	MethodOptions  = "OPTIONS"
	MethodDescribe = "DESCRIBE"
	MethodSetup    = "SETUP"
	MethodPlay     = "PLAY"
	MethodTeardown = "TEARDOWN"
	MethodAnnounce = "ANNOUNCE"
	MethodRecord   = "RECORD"
)

type RTSPRequest struct {
	Method    string
	URL       string
	Version   string
	Headers   map[string]string
	Session   string
	Sequence  int
	Transport string
	Require   string
	UserAgent string
	Body      string
}

type RTSPResponse struct {
	Version    string
	StatusCode int
	StatusText string
	Headers    map[string]string
	Body       string
}

func ParseRTSPRequest(data string) *RTSPRequest {
	lines := strings.Split(data, "\r\n")
	if len(lines) < 1 {
		return nil
	}

	// Parse request line
	parts := strings.Split(lines[0], " ")
	if len(parts) < 3 {
		return nil
	}

	req := &RTSPRequest{
		Method:  parts[0],
		URL:     parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}
	idx := strings.Index(req.URL, "streamid=")
	if idx != -1 {
		req.Session = req.URL[idx+len("streamid="):]
	}

	// Parse headers
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			break
		}

		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			req.Headers[key] = value

			switch key {
			case "CSeq":
				// Parse sequence number
			case "Transport":
				req.Transport = value
			case "Require":
				req.Require = value
			case "User-Agent":
				req.UserAgent = value
			}
		}
	}

	return req
}

func BuildRTSPResponse(statusCode int, statusText string, headers map[string]string, body string) string {
	response := fmt.Sprintf("RTSP/1.0 %d %s\r\n", statusCode, statusText)

	// Add headers
	if headers != nil {
		for key, value := range headers {
			response += key + ": " + value + "\r\n"
		}
	}

	// Add body if present
	if body != "" {
		response += "Content-Length: " + strconv.Itoa(len(body)) + "\r\n"
		response += "\r\n"
		response += body
	} else {
		response += "\r\n"
	}

	return response
}
