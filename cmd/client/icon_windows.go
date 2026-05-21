//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
)

func trayIconICO() []byte {
	const (
		w           = 32
		h           = 32
		xorSize     = w * h * 4
		maskStride  = ((w + 31) / 32) * 4
		maskSize    = maskStride * h
		imageSize   = xorSize + maskSize
		headerSize  = 40
		bytesInRes  = headerSize + imageSize
		imageOffset = 6 + 16
	)

	var out bytes.Buffer
	write16(&out, 0)  // reserved
	write16(&out, 1)  // icon
	write16(&out, 1)  // one image
	out.WriteByte(w)  // width
	out.WriteByte(h)  // height
	out.WriteByte(0)  // colors
	out.WriteByte(0)  // reserved
	write16(&out, 1)  // planes
	write16(&out, 32) // bit count
	write32(&out, bytesInRes)
	write32(&out, imageOffset)

	write32(&out, headerSize)
	write32(&out, w)
	write32(&out, h*2) // XOR bitmap + AND mask
	write16(&out, 1)
	write16(&out, 32)
	write32(&out, 0)
	write32(&out, imageSize)
	write32(&out, 0)
	write32(&out, 0)
	write32(&out, 0)
	write32(&out, 0)

	xor := make([]byte, xorSize)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x-15, y-15
			dist2 := dx*dx + dy*dy
			if dist2 > 15*15 {
				continue
			}
			r, g, b, a := byte(0x17), byte(0x69), byte(0xaa), byte(0xff)
			if dist2 > 13*13 {
				r, g, b = 0x0d, 0x47, 0x79
			}
			if (y >= 8 && y <= 11 && x >= 8 && x <= 23) || (x >= 14 && x <= 17 && y >= 8 && y <= 24) {
				r, g, b = 0xff, 0xff, 0xff
			}
			if y >= 20 && y <= 23 && x >= 10 && x <= 21 {
				r, g, b = 0x84, 0xd9, 0xff
			}
			row := h - 1 - y
			i := (row*w + x) * 4
			xor[i+0] = b
			xor[i+1] = g
			xor[i+2] = r
			xor[i+3] = a
		}
	}
	out.Write(xor)
	out.Write(make([]byte, maskSize))
	return out.Bytes()
}

func write16(b *bytes.Buffer, v uint16) {
	_ = binary.Write(b, binary.LittleEndian, v)
}

func write32(b *bytes.Buffer, v uint32) {
	_ = binary.Write(b, binary.LittleEndian, v)
}
