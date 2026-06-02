# unlock-music-go

一个用 Go 编写的命令行工具，用于解密中国各大流媒体平台（网易云音乐、QQ 音乐、酷狗音乐、酷我音乐、喜马拉雅等）的加密音乐文件，并可按需将 LRC 歌词嵌入解密后的音频标签。

---

## 功能特性

- 支持 30+ 种加密格式，覆盖主流中国音乐平台
- 可选检测同目录下的 `.lrc` 歌词文件并嵌入音频标签（MP3 → ID3v2 USLT；FLAC / OGG → Vorbis Comment）
- NCM 解密会将容器内封面写回 MP3 / FLAC / OGG 输出标签
- 批量模式：递归处理整个目录树
- 独立歌词嵌入模式：为未加密的 MP3、FLAC、OGG 文件写入歌词，无需解密
- 支持正则表达式歌词匹配规则，使用 `{name}` 占位符
- 自动识别 UTF-8 / UTF-16 / GBK / GB18030 编码的歌词文件
- 容错处理：单个文件失败不影响整批任务继续执行
- 输出目录保留原始子目录结构

---

## 支持的加密格式

| 平台 | 文件扩展名 |
|---|---|
| **网易云音乐** | `.ncm` `.uc`（缓存） |
| **QQ 音乐** | `.mgg` `.mgg0` `.mggl` `.mgg1` `.mflac` `.mflac0` `.qmcflac` `.qmcogg` `.qmc0` `.qmc2` `.qmc3` `.qmc4` `.qmc6` `.qmc8` `.bkcmp3` `.bkcm4a` `.bkcflac` `.bkcwav` `.bkcape` `.bkcogg` `.bkcwma` `.tkm` `.cache` `.666c6163` `.6d7033` `.6f6767` `.6d3461` `.776176` |
| **QQ 音乐（旧版）** | `.tm2` `.tm6` |
| **酷我音乐** | `.kwm` |
| **喜马拉雅** | `.x2m` `.x3m` `.xm` |
| **酷狗音乐** | `.kgm` `.kgma` `.vpr` |
| **咪咕音乐** | `.mg3d` |

解密后的输出格式为标准的 MP3、FLAC、OGG、M4A、WAV 或 APE，具体取决于加密文件内部存储的音频格式。

### 歌词嵌入支持

歌词可嵌入 **MP3**（ID3v2.3 `USLT` 帧）、**FLAC** 和 **OGG**（Vorbis Comment `LYRICS` 字段）。其他输出格式（M4A、WAV、APE）即使存在对应的 `.lrc` 文件，也不会嵌入歌词。

### 封面保留

NCM 容器中单独保存的专辑图片会在解密时写回支持的输出格式：MP3 写入 `APIC`，FLAC 写入 `PICTURE` metadata block，OGG 写入 `METADATA_BLOCK_PICTURE`。其他输出格式暂不主动写入封面；如果封面原本就在解密后的音频 payload 内，会随 payload 保留。

---

## 安装

### 直接下载

从 Releases 页面下载对应平台的预编译二进制文件（Windows 为 `unlock.exe`）。

### 从源码编译

**环境要求：Go 1.25+**

```bash
git clone <仓库地址>
cd unlock-music-go
go build -o unlock .
```

在 Linux / macOS 上交叉编译 Windows 版本：

```bash
GOOS=windows GOARCH=amd64 go build -o unlock.exe .
```

---

## 使用方法

```
unlock-music-go -i <文件或目录> [-o <输出目录>] [-with-lyrics] [-lrc-pattern <正则>]
unlock-music-go -i <文件或目录> -embed-lyrics [-o <输出目录>] [-lrc-pattern <正则>]
unlock-music-go -i <文件.mp3|flac|ogg> -dump-tags
```

### 参数说明

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-i` | （必填） | 输入文件或目录，目录会被递归遍历 |
| `-o` | （与源文件同目录） | 输出目录，会镜像源目录的子目录结构 |
| `-lrc-pattern` | `{name}\.lrc` | 歌词文件匹配的正则模板，`{name}` 会被替换为歌曲文件名（已转义），匹配不区分大小写 |
| `-with-lyrics` | false | 解密模式下查找并嵌入匹配的 `.lrc` 歌词 |
| `-embed-lyrics` | false | 启用歌词嵌入模式（不解密，仅写入歌词） |
| `-dump-tags` | false | 打印 MP3、FLAC 或 OGG 文件中已嵌入的歌词内容，然后退出 |

---

### 模式一：解密模式（默认）

解密 `-i` 路径下所有支持的加密文件。默认只输出音频，不写入歌词。需要歌词时，加 `-with-lyrics`，程序会在**同目录**下按 `-lrc-pattern` 规则查找歌词文件，找到则嵌入。

```powershell
# 解密单个文件
unlock.exe -i "周杰伦 - 最长的电影.mflac"

# 批量解密整个目录，输出到 D:\output
unlock.exe -i D:\Music -o D:\output

# 解密时写入歌词
unlock.exe -i D:\Music -o D:\output -with-lyrics

# 使用宽松的歌词匹配规则写入歌词
unlock.exe -i D:\Music -o D:\output -with-lyrics -lrc-pattern "{name}.*\.lrc"
```

---

### 模式二：歌词嵌入模式（`-embed-lyrics`）

将歌词嵌入**未加密**的 MP3、FLAC 或 OGG 文件，不执行任何解密操作。没有找到对应歌词文件的音频文件会被静默跳过。

> **注意：** 不指定 `-o` 时，会**直接覆盖原文件**。建议配合 `-o` 使用以保留原始文件。

```powershell
# 就地写入歌词（覆盖原文件）
unlock.exe -i D:\Music -embed-lyrics

# 写入到输出目录，不修改原文件
unlock.exe -i D:\Music -embed-lyrics -o D:\output

# 自定义歌词匹配规则
unlock.exe -i D:\Music -embed-lyrics -lrc-pattern "{name}[ ._-]*\.lrc"
```

---

### 模式三：查看已嵌入歌词（`-dump-tags`）

读取并打印 MP3、FLAC 或 OGG 文件中已嵌入的歌词文本，用于验证歌词是否写入成功。

```powershell
unlock.exe -i song.mp3 -dump-tags
unlock.exe -i song.flac -dump-tags
unlock.exe -i song.ogg -dump-tags
```

---

### 输出示例

**解密模式：**
```
  OK    周杰伦 - 最长的电影.mflac  →  周杰伦 - 最长的电影.flac
  OK    烟把儿乐队 - 纸短情长.mgg  →  烟把儿乐队 - 纸短情长.ogg
  OK    song.ncm                   →  song.mp3
  FAIL  broken.qmc0
        └─ qmc: key derivation failed: ...

━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Total   : 4
  Success : 3
  Failed  : 1
    • broken.qmc0
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**歌词嵌入模式：**
```
  OK    周杰伦 - 晴天.mp3        →  周杰伦 - 晴天.mp3        [+lrc]
  OK    周杰伦 - 稻香.flac       →  周杰伦 - 稻香.flac       [+lrc]
  OK    烟把儿乐队 - 纸短情长.ogg →  烟把儿乐队 - 纸短情长.ogg [+lrc]

━━━ Summary ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Scanned : 6
  Success : 3  (lyrics embedded: 3)
  Skipped : 3  (no matching lyrics file)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## 歌词文件检测规则

`-lrc-pattern` 是一个 Go 正则表达式，其中 `{name}` 会被替换为歌曲文件名（已对正则特殊字符转义）。匹配范围为完整文件名（含扩展名），不区分大小写。

如果宽松规则匹配到多份歌词，程序会优先使用精确的 `歌曲名.lrc`；如果没有精确匹配，会跳过并提示，避免把翻译版、Live 版或其他同名前缀歌词误写进音频。

| 规则 | 匹配示例 |
|---|---|
| `{name}\.lrc`（默认） | `周杰伦 - 晴天.lrc` |
| `{name}[ ._-]*\.lrc` | `周杰伦 - 晴天.lrc`、`周杰伦 - 晴天_.lrc` |
| `{name}.*\.lrc` | 任何以歌曲名开头的 `.lrc` 文件 |

歌词文件编码支持：**UTF-8**（含 BOM）、**UTF-16 LE/BE**（含 BOM）、**GBK / GB18030**（Windows 上常见的中文编码，无 BOM），均会自动解码为文本后再按目标音频标签格式嵌入。

---

## 项目结构

```
unlock-music-go/
├── main.go          # 命令行入口与模式分发
├── usage.go         # 命令行帮助文本
├── main_test.go     # 命令行辅助逻辑测试
├── types.go         # 顶层任务类型与支持的扩展名集合
├── run_modes.go     # 解密模式与歌词嵌入模式的处理流程
├── files.go         # 文件收集、歌词匹配、输出路径计算
├── output.go        # 进度与汇总输出
├── decrypt_dispatch.go # 加密格式到解密器的分发
├── encoding.go      # 歌词文件编码检测与文本解码
├── validate_ogg.go  # OGG 页面诊断工具（go:build ignore，不参与正常构建）
├── go.mod
├── go.sum
└── decrypt/
    ├── detect.go    # 音频格式嗅探（SniffAudioExt）
    ├── cover.go     # 封面嵌入：MP3（APIC）/ FLAC（PICTURE）/ OGG（METADATA_BLOCK_PICTURE）
    ├── cover_test.go
    ├── lyrics.go    # 歌词嵌入：MP3（ID3v2 USLT）/ FLAC / OGG（Vorbis Comment）
    ├── lyrics_test.go
    ├── tags_read.go # 读取 / 打印 MP3 / FLAC / OGG 中已嵌入的歌词
    ├── ncm.go       # 网易云音乐（.ncm）
    ├── ncmcache.go  # 网易云缓存（.uc）
    ├── qmc.go       # QQ 音乐总调度（QTag / STag / 旧版）
    ├── qmc_cipher.go# QMC 流密码（Static / Map / RC4）
    ├── qmc_key.go   # QMC V2 密钥派生（Tencent TEA-CBC + base64）
    ├── qmccache.go  # QQ 音乐缓存（.cache）
    ├── tea.go       # TEA 分组密码（QMC 密钥派生依赖）
    ├── tm.go        # QQ 音乐旧格式（.tm2 .tm6）
    ├── kwm.go       # 酷我音乐（.kwm）
    ├── xm.go        # 喜马拉雅旧版（.xm）
    ├── ximalaya.go  # 喜马拉雅（.x2m .x3m）
    ├── kgm.go       # 酷狗音乐（.kgm .kgma .vpr）
    └── mg3d.go      # 咪咕音乐（.mg3d）
```

---

## 依赖

| 包 | 用途 |
|---|---|
| `golang.org/x/text` | GBK / GB18030 解码，用于处理 Windows 下的中文歌词文件 |

所有解密逻辑均为原生实现，无其他外部依赖。

---

## 免责声明

本项目仅供个人学习和研究使用，请遵守各音乐平台的用户服务协议，勿将解密内容用于商业用途或二次传播。
