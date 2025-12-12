package transport

import (
	"net"
	"strconv"
)

type UDPServer struct {
	conn *net.UDPConn
}

func NewUDPServer(port int) (*UDPServer, error) {
	addr := &net.UDPAddr{
		IP:   net.IPv4(0, 0, 0, 0),
		Port: port,
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	conn.SetWriteBuffer(4 * 1024 * 1024)

	return &UDPServer{
		conn: conn,
	}, nil
}

func (s *UDPServer) Conn() *net.UDPConn {
	return s.conn
}

func (s *UDPServer) ReadFrom(buffer []byte) (int, *net.UDPAddr, error) {
	return s.conn.ReadFromUDP(buffer)
}

func (s *UDPServer) WriteTo(data []byte, addr *net.UDPAddr) error {
	_, err := s.conn.WriteToUDP(data, addr)
	return err
}

func (s *UDPServer) LocalPort() int {
	if s.conn == nil {
		return 0
	}
	addr := s.conn.LocalAddr().(*net.UDPAddr)
	return addr.Port
}

func (s *UDPServer) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func FindAvailableUDPPort(startPort int) (int, error) {
	for port := startPort; port < startPort+100; port++ {
		addr := &net.UDPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: port,
		}

		conn, err := net.ListenUDP("udp", addr)
		if err == nil {
			conn.Close()
			return port, nil
		}
	}
	return 0, &net.AddrError{Err: "no available port", Addr: strconv.Itoa(startPort)}
}
