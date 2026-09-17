package bgmimages

import (
	"bufio"
	"fmt"
	"io"
)

func jpegSize(r io.Reader) (w, h int, err error) {
	br := bufio.NewReader(r)
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil {
		return 0, 0, fmt.Errorf("jpeg soi: %w", err)
	}
	if soi[0] != 0xff || soi[1] != 0xd8 {
		return 0, 0, fmt.Errorf("jpeg: missing soi")
	}
	for {
		marker, err := readJPEGMarker(br)
		if err != nil {
			return 0, 0, err
		}
		switch {
		case marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7):
			continue
		case marker == 0xd9 || marker == 0xda:
			return 0, 0, fmt.Errorf("jpeg: no frame header")
		case isJPEGFrame(marker):
			if _, err := readJPEGUint16(br); err != nil {
				return 0, 0, fmt.Errorf("jpeg frame length: %w", err)
			}
			if _, err := br.ReadByte(); err != nil {
				return 0, 0, fmt.Errorf("jpeg frame: %w", err)
			}
			height, err := readJPEGUint16(br)
			if err != nil {
				return 0, 0, fmt.Errorf("jpeg frame: %w", err)
			}
			width, err := readJPEGUint16(br)
			if err != nil {
				return 0, 0, fmt.Errorf("jpeg frame: %w", err)
			}
			if width == 0 || height == 0 {
				return 0, 0, fmt.Errorf("jpeg: zero dimension")
			}
			return int(width), int(height), nil
		default:
			n, err := readJPEGUint16(br)
			if err != nil {
				return 0, 0, fmt.Errorf("jpeg segment length: %w", err)
			}
			if n < 2 {
				return 0, 0, fmt.Errorf("jpeg: segment length %d", n)
			}
			if _, err := io.CopyN(io.Discard, br, int64(n-2)); err != nil {
				return 0, 0, fmt.Errorf("jpeg segment: %w", err)
			}
		}
	}
}

func readJPEGMarker(br *bufio.Reader) (byte, error) {
	b, err := br.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("jpeg marker: %w", err)
	}
	if b != 0xff {
		return 0, fmt.Errorf("jpeg: expected marker")
	}
	for {
		b, err = br.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("jpeg marker: %w", err)
		}
		if b != 0xff {
			return b, nil
		}
	}
}

func isJPEGFrame(m byte) bool {
	return m >= 0xc0 && m <= 0xcf && m != 0xc4 && m != 0xc8 && m != 0xcc
}

func readJPEGUint16(br *bufio.Reader) (uint16, error) {
	var buf [2]byte
	if _, err := io.ReadFull(br, buf[:]); err != nil {
		return 0, err
	}
	return uint16(buf[0])<<8 | uint16(buf[1]), nil
}
