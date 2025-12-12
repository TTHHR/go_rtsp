package transport

import (
	"net"
	"time"
)

type TCPServer struct {
	listener net.Listener
	Handler  func(conn net.Conn)
}

func NewTCPServer(addr string) (*TCPServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	return &TCPServer{
		listener: listener,
	}, nil
}

func (s *TCPServer) Start() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			continue
		}

		if s.Handler != nil {
			go s.Handler(conn)
		}
	}
}

func (s *TCPServer) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func SendTCPData(conn net.Conn, data []byte, timeout time.Duration) error {
	if timeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(timeout))
	}
	_, err := conn.Write(data)
	return err
}
