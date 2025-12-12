package rtp

import (
	"net"
)

type RTPSender struct {
	conn        *net.UDPConn
	sequenceNum uint32
}

func NewRTPSender(conn *net.UDPConn) *RTPSender {
	return &RTPSender{
		conn:        conn,
		sequenceNum: 0,
	}
}

func (s *RTPSender) SendRawData(data []byte, addr *net.UDPAddr) error {
	// fmt.Printf("%x %x %x %x %x %x %x %x\n", data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7])
	// fmt.Printf("%x %x %x %x %x %x %x %x\n", data[8], data[9], data[10], data[11], data[12], data[13], data[14], data[15])
	_, err := s.conn.WriteToUDP(data, addr)
	return err
}

func (s *RTPSender) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}
