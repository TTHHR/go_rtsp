package utils

import (
	"bufio"
	"errors"
	"io"
	"os"
)

type H265NALU struct {
	Data    []byte
	Size    int
	NALType uint8
}

type H265FileReader struct {
	file    *os.File
	reader  *bufio.Reader
	running bool
}

func NewH265FileReader(filename string) (*H265FileReader, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	return &H265FileReader{
		file:    f,
		reader:  bufio.NewReader(f),
		running: false,
	}, nil
}

func (r *H265FileReader) Start() error {
	r.running = true
	return nil
}

func (r *H265FileReader) Stop() {
	r.running = false
	if r.file != nil {
		r.file.Close()
	}
}

func (r *H265FileReader) Reset() error {
	if r.file == nil {
		return errors.New("no file")
	}
	_, err := r.file.Seek(0, 0)
	if err != nil {
		return err
	}
	r.reader = bufio.NewReader(r.file)
	return nil
}

// helper: 检查 3 或 4 字节 start code（0x000001 或 0x00000001）
func hasStartCodePrefix(b []byte) bool {
	if len(b) >= 3 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x01 {
		return true
	}
	if len(b) >= 4 && b[0] == 0x00 && b[1] == 0x00 && b[2] == 0x00 && b[3] == 0x01 {
		return true
	}
	return false
}

// ReadNextNALU 返回的 Data **不包含**起始码（start code），仅包含 NAL header + payload
func (r *H265FileReader) ReadNextNALU() (*H265NALU, error) {
	if !r.running {
		return nil, errors.New("reader not running")
	}

	// 找到下一个 start code（跳过可能的前导任意字节，直到遇到 start code）
	found := false
	for !found {
		// 读1字节并检测后续是否是 start code
		b, err := r.reader.ReadByte()
		if err != nil {
			return nil, err
		}

		if b != 0x00 {
			// 不是 0x00，继续（可能文件刚开始时就不是起始码，继续读）
			continue
		}

		// peek 3 个字节来判断是否是 00 00 01 或 00 00 00 01
		peek, err := r.reader.Peek(3)
		if err != nil {
			// 如果到 EOF，则说明文件结束或不完整
			if err == io.EOF {
				// 不能形成 start code，继续返回 EOF
				return nil, io.EOF
			}
			return nil, err
		}
		// 我们已经读了一个 0x00，所以要判断的是：我们刚读的 0x00 + peek 是否构成 startcode
		if peek[0] == 0x00 && peek[1] == 0x01 {
			// 情况：刚读的 0x00 + peek[0]==0x00? 这里表明实际是 00 01（不合法），一般不会
			// 为稳健起见，不认为是 start code，继续循环
			// 回退 peek 不需要，因为 peek 没有消耗
			continue
		}
		// 现在检查 3 字节 start code (00 00 01)
		// 已经读了一个 0x00，还需要再读两个字节来确认
		b1, _ := r.reader.ReadByte()
		b2, err := r.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, err
		}
		if b1 == 0x00 && b2 == 0x01 {
			// 找到 00 00 01
			found = true
			break
		}
		if b1 == 0x00 && b2 == 0x00 {
			// 有可能是 00 00 00 01，需要继续读一个字节
			b3, err := r.reader.ReadByte()
			if err != nil {
				if err == io.EOF {
					return nil, io.EOF
				}
				return nil, err
			}
			if b3 == 0x01 {
				// 找到 00 00 00 01
				found = true
				break
			}
			// 否则不是 start code，继续从头搜索（把读过的字节都当成数据丢弃）
		}
		// 否则继续搜索
	}

	// 现在 reader 游标在 startcode 之后的位置（即第一个 NAL 的第一个字节）
	// 读取直到下一个 start code 为止
	var data []byte
	for {
		peekN, err := r.reader.Peek(4)
		if err != nil {
			if err == io.EOF {
				// 把剩余全部读出作为最后一个 NALU
				rest, _ := io.ReadAll(r.reader)
				data = append(data, rest...)
				break
			}
			return nil, err
		}

		// 如果 peek 到下一个 start code（3 or 4 bytes）
		if hasStartCodePrefix(peekN) {
			// 到了下一个 start code 的开始（但我们并不 consume startcode），退出，保留数据为当前 NALU
			break
		}

		// 不是 start code，读一个字节并加入 data
		b, err := r.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		data = append(data, b)
	}

	if len(data) < 2 {
		// 不应该出现太短的 NALU
		return nil, io.EOF
	}

	// 解析 NAL header（HEVC 的 NAL header 是 2 字节）
	nalUnitHeader := uint16(data[0])<<8 | uint16(data[1])
	nalType := uint8((nalUnitHeader >> 9) & 0x3F)

	return &H265NALU{
		Data:    data, // 不带 startcode
		Size:    len(data),
		NALType: nalType,
	}, nil
}
