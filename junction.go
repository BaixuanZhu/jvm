package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 本文件用 Windows reparse point 原生 API 实现 junction (目录联接)。
// 不调用 cmd.exe / powershell.exe, 完全走 DeviceIoControl, 避免命令注入面。
//
// 关键点: 先用 os.Mkdir 建空目录, 再用 CreateFile 打开它 (FILE_FLAG_BACKUP_SEMANTICS
// 打开目录, FILE_FLAG_OPEN_REPARSE_POINT 让操作针对 reparse point 本身), 然后
// FSCTL_SET_REPARSE 写入 REPARSE_DATA_BUFFER。

const (
	// FILE_FLAG_BACKUP_SEMANTICS: 打开目录必须用
	// FILE_FLAG_OPEN_REPARSE_POINT: 操作 reparse point 本身而非目标
	flagBackupSemantics   = 0x02000000
	flagOpenReparsePoint  = 0x00200000
	// GENERIC_WRITE
	genericWrite          = 0x40000000
	fileShareAll          = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	openExisting          = 3
	// IO_REPARSE_TAG_MOUNT_POINT
	reparseTagMountPoint  = 0xA0000003
	// FSCTL_SET_REPARSE_POINT
	fsctlSetReparsePoint  = 0x000900A4
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
	// PathBuffer 是变长字段, 我们用固定大小数组, 实际只用到前面一部分
	PathBuffer [maxPath]uint16
}

const maxPath = 512

// createJunction 在 link 处创建指向 target 的 junction
// link 必须不存在; target 必须存在且是目录
func createJunction(link, target string) error {
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
	//    两者在 PathBuffer 中都带 NUL 终止符。
	//    各 Length 字段 = 名字 UTF16 字节数 (不含 NUL), 偏移字段 = 相对 PathBuffer 的字节位置。
	substituteName := `\??\` + absTarget
	printName := absTarget

	subUTF16 := windows.StringToUTF16(substituteName) // 含结尾 NUL
	printUTF16 := windows.StringToUTF16(printName)    // 含结尾 NUL

	subLen := uint16(len(substituteName) * 2) // 不含 NUL 的字节长度
	printLen := uint16(len(printName) * 2)

	// 3. 填充 REPARSE_DATA_BUFFER
	var buf reparseDataBuffer
	buf.ReparseTag = reparseTagMountPoint
	// SubstituteName 放在 PathBuffer 开头
	buf.SubstituteNameOffset = 0
	buf.SubstituteNameLength = subLen
	// PrintName 紧跟在 SubstituteName + 其 NUL 之后
	buf.PrintNameOffset = subLen + 2 // +2 跳过 substitute 的 NUL 终止符
	buf.PrintNameLength = printLen

	// 拷贝: substitute(含NUL) + printName(含NUL)
	idx := 0
	for i := 0; i < len(subUTF16) && idx < maxPath; i++ {
		buf.PathBuffer[idx] = subUTF16[i]
		idx++
	}
	for i := 0; i < len(printUTF16) && idx < maxPath; i++ {
		buf.PathBuffer[idx] = printUTF16[i]
		idx++
	}
	// ReparseDataLength = 8 (四个 uint16 offset/length 字段) + PathBuffer 已用字节数
	pathBufferUsed := int(subLen) + 2 + int(printLen) + 2 // sub + NUL + print + NUL
	buf.ReparseDataLength = uint16(8 + pathBufferUsed)

	// 4. 用 CreateFile 打开目录 (backup semantics + reparse point)
	linkPtr, err := windows.UTF16PtrFromString(absLink)
	if err != nil {
		os.Remove(absLink)
		return err
	}
	handle, err := windows.CreateFile(
		linkPtr,
		genericWrite,
		fileShareAll,
		nil,
		openExisting,
		flagBackupSemantics|flagOpenReparsePoint,
		0,
	)
	if err != nil {
		os.Remove(absLink)
		return fmt.Errorf("CreateFile 失败: %w", err)
	}
	defer windows.CloseHandle(handle)

	// 5. DeviceIoControl FSCTL_SET_REPARSE_POINT
	// 输入缓冲区总大小 = 8 (header: tag+len+reserved) + ReparseDataLength
	totalSize := uint32(8 + buf.ReparseDataLength)
	var bytesReturned uint32
	err = windows.DeviceIoControl(
		handle,
		fsctlSetReparsePoint,
		(*byte)(unsafe.Pointer(&buf)),
		totalSize,
		nil, 0,
		&bytesReturned,
		nil,
	)
	if err != nil {
		os.Remove(absLink)
		return fmt.Errorf("FSCTL_SET_REPARSE_POINT 失败: %w", err)
	}
	return nil
}

// removeJunction 删除 junction (只删链接本身, 不影响目标目录)
func removeJunction(link string) error {
	return os.Remove(link)
}

// readJunctionTarget 返回 current 当前指向的绝对路径; 没有则返回 ""
func readJunctionTarget() string {
	target, err := os.Readlink(currentLink)
	if err != nil {
		return ""
	}
	return target
}

// safeVersionDir 校验解压出的顶层目录名是否安全 (只允许字母数字._+-)
var safeVersionDir = regexp.MustCompile(`^[A-Za-z0-9._+\-]+$`)

// listLocalVersions 扫描 versionsDir, 返回已安装的版本目录名 (降序)
// 同时返回 current 当前指向的目录名 (没装则 "")
func listLocalVersions() (names []string, currentTarget string) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return nil, ""
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "jdk-") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	if t := readJunctionTarget(); t != "" {
		currentTarget = filepath.Base(t)
	}
	return names, currentTarget
}

// findInstalledByMajor 查找某个大版本是否已安装 (返回目录名, 没有则 "")
func findInstalledByMajor(major int) string {
	names, _ := listLocalVersions()
	for _, n := range names {
		if matchMajor(n, major) {
			return n
		}
	}
	return ""
}

// matchMajor 判断目录名 (如 jdk-21.0.5+11) 是否属于某大版本
func matchMajor(dir string, major int) bool {
	ver := strings.TrimPrefix(dir, "jdk-")
	parts := strings.FieldsFunc(ver, func(r rune) bool {
		return r == '.' || r == '+' || r == '-'
	})
	if len(parts) == 0 {
		return false
	}
	return parts[0] == fmt.Sprintf("%d", major)
}

// resolveVersion 模糊匹配用户输入到实际目录名
// 例如 "21" -> "jdk-21.0.5+11"; 输入完整目录名也接受
func resolveVersion(input string) (string, error) {
	names, _ := listLocalVersions()
	if len(names) == 0 {
		return "", fmt.Errorf("还没有安装任何版本, 请先 jvm install <版本号>")
	}

	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "jdk-")
	if input == "" {
		return "", fmt.Errorf("版本号不能为空")
	}

	// 1. 精确匹配目录名
	for _, n := range names {
		if n == "jdk-"+input || n == input {
			return n, nil
		}
	}
	// 2. 按大版本匹配 (取最新)
	var cands []string
	for _, n := range names {
		if strings.TrimPrefix(n, "jdk-") == input ||
			strings.HasPrefix(strings.TrimPrefix(n, "jdk-"), input+".") ||
			strings.HasPrefix(strings.TrimPrefix(n, "jdk-"), input+"+") {
			cands = append(cands, n)
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("没有找到匹配 '%s' 的版本。运行 jvm list 查看已安装版本", input)
	}
	return cands[0], nil // names 已降序, cands 也保持降序
}
