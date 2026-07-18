package main

import (
	"fmt"

	"unlock-music-go/decrypt"
)

func decryptFile(data []byte, ext string, qqMusicOptions decrypt.QQMusicOptions) ([]byte, string, error) {
	switch ext {
	case "ncm":
		r, err := decrypt.DecryptNcm(data)
		if err != nil {
			return nil, "", err
		}
		audio, err := embedNcmCover(r.Audio, r.Ext, r.Cover)
		if err != nil {
			return nil, "", err
		}
		return audio, r.Ext, nil

	case "uc":
		audio := decrypt.DecryptNcmCache(data)
		return audio, decrypt.SniffAudioExt(audio), nil

	case "cache":
		audio := decrypt.DecryptQmcCache(data)
		return audio, decrypt.SniffAudioExt(audio), nil

	case "mgg", "mgg0", "mggl", "mgg1",
		"mflac", "mflac0",
		"mmp4",
		"qmcflac", "qmcogg",
		"qmc0", "qmc2", "qmc3", "qmc4", "qmc6", "qmc8",
		"bkcmp3", "bkcm4a", "bkcflac", "bkcwav", "bkcape", "bkcogg", "bkcwma",
		"tkm", "666c6163", "6d7033", "6f6767", "6d3461", "776176":
		// Current QQ Music desktop downloads use the musicex footer.  Their
		// payload cipher is QMC, but its ekey lives in Checkccae.dat rather
		// than at the end of the file, so detect it before legacy QMC parsing.
		var r *decrypt.QmcResult
		var err error
		// Test the magic first, then let DecryptQQMusicEx reject unsupported
		// musicex versions.  A newer v2 footer must never fall through to the
		// legacy QMC code and produce a misleading output file.
		if decrypt.HasQQMusicExFooter(data) {
			r, err = decrypt.DecryptQQMusicEx(data, ext, qqMusicOptions)
		} else {
			r, err = decrypt.DecryptQmc(data, ext)
		}
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "tm2", "tm6":
		r, err := decrypt.DecryptTm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "kwm":
		r, err := decrypt.DecryptKwm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "xm":
		r, err := decrypt.DecryptXm(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "kgm", "kgma":
		r, err := decrypt.DecryptKgm(data, false)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "vpr":
		r, err := decrypt.DecryptKgm(data, true)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "x2m":
		r, err := decrypt.DecryptX2M(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "x3m":
		r, err := decrypt.DecryptX3M(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, r.Ext, nil

	case "mg3d":
		r, err := decrypt.DecryptMg3d(data)
		if err != nil {
			return nil, "", err
		}
		return r.Audio, "wav", nil

	default:
		return nil, "", fmt.Errorf("unsupported extension: .%s", ext)
	}
}

func embedNcmCover(audio []byte, ext string, cover []byte) ([]byte, error) {
	if len(cover) == 0 {
		return audio, nil
	}
	switch ext {
	case "mp3", "flac", "ogg":
		return decrypt.EmbedCover(audio, ext, cover)
	default:
		return audio, nil
	}
}
