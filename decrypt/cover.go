package decrypt

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// EmbedCover writes front-cover image bytes into audio metadata for supported formats.
func EmbedCover(audio []byte, ext string, image []byte) ([]byte, error) {
	if len(image) == 0 {
		return audio, nil
	}
	switch strings.ToLower(ext) {
	case "mp3":
		return embedCoverMP3(audio, image)
	case "flac":
		return embedCoverFLAC(audio, image)
	case "ogg":
		return embedCoverOGG(audio, image)
	default:
		return nil, fmt.Errorf("cover embedding is not supported for .%s files", ext)
	}
}

func embedCoverMP3(audio, image []byte) ([]byte, error) {
	tag, present, err := id3ReadTag(audio, map[string]bool{"APIC": true})
	if err != nil {
		return nil, err
	}
	if !present {
		tag = id3Tag{major: 3}
	}
	apicFrame, err := id3v2BuildAPICForVersion(tag.major, detectImageMime(image), image)
	if err != nil {
		return nil, err
	}
	tag.frames = append(tag.frames, apicFrame)
	return id3WriteTag(tag.major, tag.frames, audio[tag.audioOffset:])
}

func id3v2BuildAPIC(mime string, image []byte) []byte {
	frame, _ := id3v2BuildAPICForVersion(3, mime, image)
	return frame
}

func id3v2BuildAPICForVersion(major byte, mime string, image []byte) ([]byte, error) {
	if major == 2 {
		var data bytes.Buffer
		data.WriteByte(0x00) // ISO-8859-1
		data.WriteString(id3v22PictureFormat(mime))
		data.WriteByte(0x03) // front cover
		data.WriteByte(0x00) // empty description
		data.Write(image)
		return id3BuildFrame(2, "PIC", data.Bytes())
	}
	var data bytes.Buffer
	data.WriteByte(0x00) // ISO-8859-1 fields for MIME and empty description.
	data.WriteString(mime)
	data.WriteByte(0x00)
	data.WriteByte(0x03) // front cover
	data.WriteByte(0x00) // empty description
	data.Write(image)

	return id3BuildFrame(major, "APIC", data.Bytes())
}

func id3v22PictureFormat(mime string) string {
	switch mime {
	case "image/jpeg":
		return "JPG"
	case "image/png":
		return "PNG"
	case "image/gif":
		return "GIF"
	case "image/bmp":
		return "BMP"
	default:
		return "XXX"
	}
}

func embedCoverFLAC(audio, image []byte) ([]byte, error) {
	if len(audio) < 4 || string(audio[0:4]) != "fLaC" {
		return nil, errors.New("flac: invalid magic header")
	}

	type block struct {
		typ  byte
		data []byte
	}

	pos := 4
	var blocks []block
	for {
		if pos+4 > len(audio) {
			return nil, errors.New("flac: metadata block header missing")
		}
		header := audio[pos]
		blockType := header & 0x7F
		isLast := header&0x80 != 0
		blockSize := int(audio[pos+1])<<16 | int(audio[pos+2])<<8 | int(audio[pos+3])
		dataStart := pos + 4
		dataEnd := dataStart + blockSize
		if dataEnd > len(audio) {
			return nil, errors.New("flac: metadata block extends past end of file")
		}
		if blockType != 6 {
			dataCopy := make([]byte, blockSize)
			copy(dataCopy, audio[dataStart:dataEnd])
			blocks = append(blocks, block{typ: blockType, data: dataCopy})
		}
		pos = dataEnd
		if isLast {
			break
		}
	}

	blocks = append(blocks, block{typ: 6, data: flacPictureBlock(detectImageMime(image), image)})

	var out bytes.Buffer
	out.WriteString("fLaC")
	for i, b := range blocks {
		if len(b.data) > 0xFFFFFF {
			return nil, errors.New("flac: metadata block too large")
		}
		header := b.typ
		if i == len(blocks)-1 {
			header |= 0x80
		}
		out.WriteByte(header)
		out.Write([]byte{byte(len(b.data) >> 16), byte(len(b.data) >> 8), byte(len(b.data))})
		out.Write(b.data)
	}
	out.Write(audio[pos:])
	return out.Bytes(), nil
}

func embedCoverOGG(audio, image []byte) ([]byte, error) {
	picture := base64.StdEncoding.EncodeToString(flacPictureBlock(detectImageMime(image), image))
	return oggModifyCommentTag(audio, "METADATA_BLOCK_PICTURE", picture)
}

func flacPictureBlock(mime string, image []byte) []byte {
	var buf bytes.Buffer
	writeBE32 := func(n uint32) { binary.Write(&buf, binary.BigEndian, n) } //nolint:errcheck

	writeBE32(3) // front cover
	writeBE32(uint32(len(mime)))
	buf.WriteString(mime)
	writeBE32(0) // description length
	writeBE32(0) // width unknown
	writeBE32(0) // height unknown
	writeBE32(0) // color depth unknown
	writeBE32(0) // indexed colors
	writeBE32(uint32(len(image)))
	buf.Write(image)
	return buf.Bytes()
}

func detectImageMime(image []byte) string {
	switch {
	case bytes.HasPrefix(image, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(image, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png"
	case bytes.HasPrefix(image, []byte{'G', 'I', 'F', '8'}):
		return "image/gif"
	case bytes.HasPrefix(image, []byte{'B', 'M'}):
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}
