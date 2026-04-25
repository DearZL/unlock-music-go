//go:build ignore

package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	pos := 0
	pageNum := 0
	for pos < len(data) {
		if pos+27 > len(data) {
			fmt.Printf("页%d: 文件截断\n", pageNum)
			break
		}
		if string(data[pos:pos+4]) != "OggS" {
			fmt.Printf("页%d @ offset %d: 失去同步! 前4字节: %x\n", pageNum, pos, data[pos:pos+4])
			break
		}

		headerType := data[pos+5]
		granule := binary.LittleEndian.Uint64(data[pos+6:])
		serial := binary.LittleEndian.Uint32(data[pos+14:])
		seq := binary.LittleEndian.Uint32(data[pos+18:])
		nseg := int(data[pos+26])

		pageDataLen := 0
		lastSeg := byte(0)
		for i := 0; i < nseg; i++ {
			s := data[pos+27+i]
			pageDataLen += int(s)
			lastSeg = s
		}

		headerLen := 27 + nseg
		dataStart := pos + headerLen
		dataEnd := dataStart + pageDataLen

		// 猜测页面类型
		pageType := "audio"
		if dataEnd <= len(data) && pageDataLen > 0 {
			d := data[dataStart:]
			switch {
			case len(d) >= 7 && d[0] == 0x01 && string(d[1:7]) == "vorbis":
				pageType = "【identification header】"
			case len(d) >= 7 && d[0] == 0x03 && string(d[1:7]) == "vorbis":
				pageType = "【comment header】"
			case len(d) >= 7 && d[0] == 0x05 && string(d[1:7]) == "vorbis":
				pageType = "【setup header】"
			case len(d) >= 8 && string(d[0:8]) == "OpusHead":
				pageType = "【Opus identification】"
			case len(d) >= 8 && string(d[0:8]) == "OpusTags":
				pageType = "【Opus comment header】"
			}
		}

		packetContinues := lastSeg == 255
		fmt.Printf("页%d @ offset %5d | type=0x%02x | serial=%d | seq=%d | granule=%d | nseg=%d | dataLen=%d | lastSeg=%d %s %s\n",
			pageNum, pos, headerType, serial, seq, granule, nseg, pageDataLen,
			lastSeg,
			func() string {
				if packetContinues {
					return "→续"
				}
				return "  "
			}(),
			pageType,
		)

		pos = dataEnd
		pageNum++
	}
}
