package main

import (
	"flag"
	"fmt"
	"os"
)

func usage() {
	fmt.Fprint(os.Stderr, `unlock-music-go —— 批量解密音乐文件并可选写入歌词

用法
  解密模式（默认）：
    unlock-music-go -i <文件或目录> [-o <输出目录>] [-with-lyrics] [-lrc-pattern <正则>]

  写入歌词模式：
    unlock-music-go -i <文件或目录> -embed-lyrics [-o <输出目录>] [-lrc-pattern <正则>]

  查看标签模式：
    unlock-music-go -i <file.mp3|flac|ogg> -dump-tags

模式说明
  （默认）        解密加密音乐文件。默认不写入歌词。
                 如需在解密后查找同目录 .lrc 并写入，请加 -with-lyrics。

  -embed-lyrics  给已有 MP3/FLAC/OGG 文件写入歌词，不执行解密。
                 适用于已经是明文音频但希望补充歌词标签的场景。
                 未匹配到歌词文件会自动跳过。

  -dump-tags     输出 MP3（USLT）或 FLAC/OGG（LYRICS）中的歌词内容并退出。

参数
`)
	flag.PrintDefaults()

	fmt.Fprint(os.Stderr, `
歌词匹配规则
  -lrc-pattern 是一个正则模板，其中 {name} 会被替换为歌曲名（已转义）。
  匹配为大小写不敏感，并默认完全匹配。

    默认：{name}\.lrc
    示例：{name}[ ._-]*\.lrc
    示例：{name}.*\.lrc

  若未找到歌词文件，不报错，继续处理。
  若宽松规则匹配到多份歌词且没有精确的“歌曲名.lrc”，会跳过并提示，避免误写。

输出
  不使用 -o 时：输出到源文件同目录
  使用 -o 时：保持目录结构输出到指定目录
  在 -embed-lyrics 模式下且未指定 -o：会直接覆盖原文件

示例
  unlock-music-go -i song.mflac
  unlock-music-go -i ./Music -o ./output
  unlock-music-go -i ./Music -with-lyrics -lrc-pattern "{name}.*\.lrc"
  unlock-music-go -i ./Music -embed-lyrics
  unlock-music-go -i ./Music -embed-lyrics -o ./output -lrc-pattern "{name}.*\.lrc"
  unlock-music-go -i song.mp3 -dump-tags
`)
}
