// Package junction 用 Windows reparse point 原生 API 实现 junction (目录联接),
// 并提供本地已安装版本的查询能力。
//
// 不调用 cmd.exe / powershell.exe, 完全走 DeviceIoControl, 避免命令注入面。
//
// 关键点: 先用 os.Mkdir 建空目录, 再用 CreateFile 打开它 (FILE_FLAG_BACKUP_SEMANTICS
// 打开目录, FILE_FLAG_OPEN_REPARSE_POINT 让操作针对 reparse point 本身), 然后
// FSCTL_SET_REPARSE 写入 REPARSE_DATA_BUFFER。
package junction

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"jvm/internal/paths"

	"golang.org/x/sys/windows"
)

const (
	// FILE_FLAG_BACKUP_SEMANTICS: 打开目录必须用
	// FILE_FLAG_OPEN_REPARSE_POINT: 操作 reparse point 本身而非目标
	flagBackupSemantics  = 0x02000000
	flagOpenReparsePoint = 0x00200000
	genericWrite         = 0x40000000 // GENERIC_WRITE
	fileShareAll         = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	openExisting         = 3
	reparseTagMountPoint = 0xA0000003 // IO_REPARSE_TAG_MOUNT_POINT
	fsctlSetReparsePoint = 0x000900A4 // FSCTL_SET_REPARSE_POINT
)

// reparseDataBuffer 对应 Windows REPARSE_DATA_BUFFER (MountPoint 变体)
// 文档: https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ns-ntifs-_reparse_data_buffer
type reparseDataBuffer struct {
	ReparseTag           uint32
	ReparseDataLength    uint16
	Reserved             uint16
	SubstituteNameOffset uint16
	SubstituteNameLength uint16
	PrintNameOffset      uint16
	PrintNameLength      uint16
	PathBuffer           [maxPath]uint16 // 变长字段, 用固定大小数组, 实际只用到前面一部分
}

const maxPath = 512

// Create 在 link 处创建指向 target 的 junction。
// link 必须不存在; target 必须存在且是目录。
func Create(link, target string) error {
	// 校验 target 存在且是目录
	ti, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("目标版本目录不存在: %w", err)
	}
	if !ti.IsDir() {
		return fmt.Errorf("目标不是目录: %s", target)
	}
	// 校验 link 不存在
	if _, err := os.Lstat(link); err == nil {
		return fmt.Errorf("链接路径已存在, 请先删除: %s", link)
	}

	absLink, _ := filepath.Abs(link)
	absTarget, _ := filepath.Abs(target)

	// 1. 先建空目录 (junction 的载体)
	if err := os.Mkdir(absLink, 0o755); err != nil {
		return fmt.Errorf("创建链接目录失败: %w", err)
	}

	// 2. 准备两个名字 (严格对齐 mklink /J 的输出格式):
	//    SubstituteName = "\??\C:\...\target"  (Win32 对象命名空间前缀)
	//    PrintName      = "C:\...\target"      (普通路径, 用户可读)
	substituteName := `\??\` + absTarget
	printName := absTarget
	subUTF16 := windows.StringToUTF16(substituteName) // 含结尾 NUL
	printUTF16 := windows.StringToUTF16(printName)
	subLen := uint16(len(substituteName) * 2) // 不含 NUL 的字节长度
	printLen := uint16(len(printName) * 2)

	// 3. 填充 REPARSE_DATA_BUFFER
	var buf reparseDataBuffer
	buf.ReparseTag = reparseTagMountPoint
	buf.SubstituteNameOffset = 0
	buf.SubstituteNameLength = subLen
	buf.PrintNameOffset = subLen + 2 // +2 跳过 substitute 的 NUL
	buf.PrintNameLength = printLen
	idx := 0
	for i := 0; i < len(subUTF16) && idx < maxPath; i++ {
		buf.PathBuffer[idx] = subUTF16[i]
		idx++
	}
	for i := 0; i < len(printUTF16) && idx < maxPath; i++ {
		buf.PathBuffer[idx] = printUTF16[i]
		idx++
	}
	pathBufferUsed := int(subLen) + 2 + int(printLen) + 2 // sub + NUL + print + NUL
	buf.ReparseDataLength = uint16(8 + pathBufferUsed)

	// 4. 用 CreateFile 打开目录 (backup semantics + reparse point)
	linkPtr, err := windows.UTF16PtrFromString(absLink)
	if err != nil {
		os.Remove(absLink)
		return err
	}
	handle, err := windows.CreateFile(
		linkPtr, genericWrite, fileShareAll, nil, openExisting,
		flagBackupSemantics|flagOpenReparsePoint, 0,
	)
	if err != nil {
		os.Remove(absLink)
		return fmt.Errorf("CreateFile 失败: %w", err)
	}
	defer windows.CloseHandle(handle)

	// 5. DeviceIoControl FSCTL_SET_REPARSE_POINT
	totalSize := uint32(8 + buf.ReparseDataLength)
	var bytesReturned uint32
	err = windows.DeviceIoControl(
		handle, fsctlSetReparsePoint,
		(*byte)(unsafe.Pointer(&buf)), totalSize,
		nil, 0, &bytesReturned, nil,
	)
	if err != nil {
		os.Remove(absLink)
		return fmt.Errorf("FSCTL_SET_REPARSE_POINT 失败: %w", err)
	}
	return nil
}

// Remove 删除 junction (只删链接本身, 不影响目标目录)。
func Remove(link string) error {
	return os.Remove(link)
}

// ReadTarget 返回 current 当前指向的绝对路径; 没有则返回 ""。
func ReadTarget() string {
	target, err := os.Readlink(paths.CurrentLink)
	if err != nil {
		return ""
	}
	return target
}

// ListLocal 扫描 versionsDir, 返回已安装的版本目录名 (降序)。
// 同时返回 current 当前指向的目录名 (没选则 "")。
func ListLocal() (names []string, currentTarget string) {
	entries, err := os.ReadDir(paths.VersionsDir)
	if err != nil {
		return nil, ""
	}
	for _, e := range entries {
		// 只认能解析出大版本号的目录 (兼容 jdk-21.0.12+8 和 jdk8u502-b07 两种命名)
		if e.IsDir() && majorOf(e.Name()) > 0 {
			names = append(names, e.Name())
		}
	}
	// 按语义版本降序 (字符串排序会把 21.0.12+8 误排在 21.0.5+11 后面)
	sort.Slice(names, func(i, j int) bool {
		return semverLess(names[j], names[i]) // 降序: j<i 时 i 在前
	})

	if t := ReadTarget(); t != "" {
		currentTarget = filepath.Base(t)
	}
	return names, currentTarget
}

// ResolveVersion 把用户输入解析到实际安装的版本目录名。规则 (刻意保持简单):
//  1. 纯大版本号 ("8" / "17" / "21"): 取该大版本下语义最新的 build。
//  2. 其他: 必须是已安装目录的完整名字 (如 "jdk-17.0.20+8"), 一字不差。
//
// 即只有"给大版本号"这一种模糊匹配; 想精确指定某个 build 就粘 jvm list 里的全名。
func ResolveVersion(input string) (string, error) {
	names, _ := ListLocal()
	if len(names) == 0 {
		return "", fmt.Errorf("还没有安装任何版本, 请先 jvm install <版本号>")
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("版本号不能为空")
	}

	// 1. 纯大版本号 → 该大版本下语义最新
	if major, ok := pureMajor(input); ok {
		var cands []string
		for _, n := range names {
			if majorOf(n) == major {
				cands = append(cands, n)
			}
		}
		if len(cands) == 0 {
			return "", fmt.Errorf("没有安装大版本 %d。运行 jvm list 查看已安装版本", major)
		}
		return latestSemver(cands), nil
	}

	// 2. 完整目录名精确匹配
	for _, n := range names {
		if n == input {
			return n, nil
		}
	}
	return "", fmt.Errorf("没有找到版本 '%s'。运行 jvm list 查看已安装版本 (需输入完整目录名, 如 jdk-17.0.20+8)", input)
}

// latestSemver 从一组版本目录名里返回语义最新的那个。
func latestSemver(names []string) string {
	best := names[0]
	for _, n := range names[1:] {
		if semverLess(best, n) {
			best = n
		}
	}
	return best
}

// majorOf 从目录名 / 版本串里解析大版本号, 解析失败返回 0。
// 兼容两种 Temurin 命名:
//   - 新式: "jdk-21.0.12+8"  → 21
//   - 旧式: "jdk8u502-b07"    → 8
//
// 纯函数, 便于表驱动测试。
func majorOf(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "jdk-")
	s = strings.TrimPrefix(s, "jdk")
	s = strings.TrimPrefix(s, "JDK-")
	s = strings.TrimPrefix(s, "JDK")
	// 取开头连续数字作为大版本号
	end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(s)
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// pureMajor 判断 s 是否为纯数字大版本号 (如 "8" / "21"), 是则返回它, 否则 ok=false。
// 这是 ResolveVersion 唯一接受的"模糊"输入形式。纯函数, 便于测试。
func pureMajor(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// versionParts 把版本目录名解析成数字段切片, 用于语义版本比较。
// 兼容两种 Temurin 命名:
//   - 新式 "jdk-21.0.12+8"  → [21, 0, 12, 8]
//   - 旧式 "jdk8u502-b07"    → [8, 502, 7]   (major, update, build)
//
// 纯函数, 便于表驱动测试。
func versionParts(s string) []int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "jdk-")
	s = strings.TrimPrefix(s, "jdk")
	s = strings.TrimPrefix(s, "JDK-")
	s = strings.TrimPrefix(s, "JDK")
	s = strings.ToLower(s)
	// 新式命名: 把 'u' (旧式 update 分隔符) 当成普通分隔符, 与 ./+ 统一拆段
	s = strings.ReplaceAll(s, "u", ".")
	var parts []int
	for _, seg := range strings.FieldsFunc(s, func(r rune) bool {
		return r < '0' || r > '9' // 用 "非数字" 作分隔符: . + - u 都会拆开
	}) {
		if seg == "" {
			continue
		}
		n, err := strconv.Atoi(seg)
		if err != nil {
			n = 0 // 非法段按 0 处理, 不打断比较
		}
		parts = append(parts, n)
	}
	return parts
}

// semverLess 按语义版本比较两个版本目录名: 返回 a 是否严格小于 b。
// 逐段比较数字段, 短的视为后续段为 0。
// 例如 "21.0.12+8" 不小于 "21.0.5+11" (12 > 5); "8u502-b07" 大于 "8u402-b06"。
// 纯函数, 便于表驱动测试。
func semverLess(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		va, vb := 0, 0
		if i < len(pa) {
			va = pa[i]
		}
		if i < len(pb) {
			vb = pb[i]
		}
		if va != vb {
			return va < vb
		}
	}
	return false // 完全相等
}
