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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"jvm/internal/app"
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
//
// 返回的是磁盘上的真实目录名 (旧目录无前缀如 "21.0.12+8", 新目录带前缀如
// "temurin-21.0.5+11")。需要统一显示时用 DisplayName 归一化。
func ListLocal() (names []string, currentTarget string) {
	entries, err := os.ReadDir(paths.VersionsDir)
	if err != nil {
		return nil, ""
	}
	for _, e := range entries {
		// 只认能解析出大版本号的目录 (含 {distro}-{version} 和旧的无前缀形式)
		if e.IsDir() && MajorOf(e.Name()) > 0 {
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

// DisplayName 把磁盘上的目录名归一化为统一的 {distro}-{version} 显示形式。
// 旧的无前缀目录 ("21.0.12+8") 补上 temurin- 前缀; 已带前缀的原样返回。
// 仅用于展示 (list 输出), 不改磁盘数据。
func DisplayName(dirName string) string {
	distro, ver := SplitDistro(dirName)
	if ver == "" {
		return dirName // 无法解析, 原样返回
	}
	return distro + "-" + ver
}

// ResolveVersion 把用户输入解析到实际安装的版本目录名。规则:
//  1. 纯大版本号 ("8" / "17" / "21"): 取该 distro 该大版本下语义最新的 build。
//  2. 完整版本号 ("25.0.4+7" / "jdk-25.0.4+7"): 精确匹配, 带不带 jdk- 前缀都行。
//
// 不接受半截版本号 (如 "25.0.4"): 不同发行版版本号格式不一 (Temurin 21.0.5+11,
// Corretto 21.0.12.8.1), 半截形式语义模糊。要装指定 patch 就输完整版本号 (含
// build 号), 大版本号取最新 —— 两种形式, 清晰可预测。
//
// distro 用于先过滤本地目录集合 (只在该发行版的目录里找);
// 旧的无 distro 前缀目录 (如 "21.0.12+8") 由 SplitDistro 视为 temurin,
// 故 distro=="temurin" 时能命中旧目录, 实现向后兼容。
func ResolveVersion(distro, input string) (string, error) {
	names, _ := ListLocal()
	if len(names) == 0 {
		return "", fmt.Errorf("还没有安装任何版本, 请先 jvm install <版本号>")
	}

	// 先按 distro 过滤: 只保留属于该发行版的目录 (含旧的无前缀目录, 视为 temurin)
	var cands []string
	for _, n := range names {
		if d, _ := SplitDistro(n); d == distro {
			cands = append(cands, n)
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("没有安装 %s 的任何版本。运行 jvm list 查看已安装版本", distro)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("版本号不能为空")
	}

	// 1. 纯大版本号 → 该 distro 该大版本下语义最新
	if major, ok := pureMajor(input); ok {
		var majorCands []string
		for _, n := range cands {
			if MajorOf(n) == major {
				majorCands = append(majorCands, n)
			}
		}
		if len(majorCands) == 0 {
			return "", fmt.Errorf("没有安装 %s 大版本 %d。运行 jvm list 查看已安装版本", distro, major)
		}
		return latestSemver(majorCands), nil
	}

	// 2. 精确匹配版本号: 带不带 jdk- 前缀都接受。
	//    与 install/available 对齐 —— 它们用不带前缀的简短形式 (如 "25.0.4+7"),
	//    所以 use/uninstall 也能这么写; 直接粘 list 里的全名 (temurin-25.0.4+7) 也行。
	want := stripPrefix(input)
	for _, n := range cands {
		if n == input || stripPrefix(n) == want {
			return n, nil
		}
	}
	return "", fmt.Errorf("没有找到 %s 版本 '%s'。用大版本号取最新, 或完整版本号 (含 build 号)。运行 jvm list 查看已安装版本", distro, input)
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

// newDirNameRe 匹配规范化的新目录名: X.Y.Z+N (纯 semver, 无 jdk- 前缀)。
var newDirNameRe = regexp.MustCompile(`^\d+\.\d+\.\d+\+\d+$`)

// legacyToNewName 把旧版 jvm 遗留的目录名映射到新的纯 semver 形式。
// 两种历史命名:
//   - jdk-21.0.12+8     → 21.0.12+8   (去 jdk- 前缀)
//   - jdk8u502-b07       → 8.0.502+7   (旧式 update/build → semver)
//
// 已经是新名 / 无法识别 → 返回 ""。
// 纯函数, 便于表驱动测试。
func legacyToNewName(name string) string {
	s := strings.TrimSpace(name)

	// 新式遗留: 去掉 jdk- 前缀后应符合 X.Y.Z+N
	if strings.HasPrefix(s, "jdk-") {
		cand := s[len("jdk-"):]
		if newDirNameRe.MatchString(cand) {
			return cand
		}
	}

	// 旧式遗留: jdk{N}u{U}-b{B}, 例如 jdk8u502-b07 → 8.0.502+7
	// 去掉 jdk 前缀, 按 u 和 -b 切三段
	if strings.HasPrefix(s, "jdk") && strings.Contains(s, "u") && strings.Contains(s, "-b") {
		rest := s[len("jdk"):] // "8u502-b07"
		uIdx := strings.IndexByte(rest, 'u')
		major := rest[:uIdx] // "8"
		rest = rest[uIdx+1:] // "502-b07"
		bIdx := strings.Index(rest, "-b")
		if bIdx < 0 {
			return ""
		}
		update := rest[:bIdx]  // "502"
		build := rest[bIdx+2:] // "07"
		m, err1 := strconv.Atoi(major)
		u, err2 := strconv.Atoi(update)
		b, err3 := strconv.Atoi(build)
		if err1 != nil || err2 != nil || err3 != nil || m <= 0 || u <= 0 || b <= 0 {
			return ""
		}
		cand := fmt.Sprintf("%d.0.%d+%d", m, u, b) // 去掉 build 的前导零
		if newDirNameRe.MatchString(cand) {
			return cand
		}
	}

	return ""
}

// MigrateLegacyDirs 把旧版 jvm 遗留的版本目录重命名为新的纯 semver 形式。
// 由 main 在自举阶段调用 (幂等, 无网络, 纯字符串规则)。
//
// 行为:
//   - 符合新规范的目录跳过 (含纯 semver 旧目录和 {distro}-{semver} 新命名)。
//   - 旧目录 (jdk-X.Y.Z+B / jdk{N}u{U}-b{B}) 就地 rename 成新名。
//   - 新名已存在 → 跳过并提示 (用户可能已装新名版本)。
//   - current 链接若指向被 rename 的旧目录, rename 后重建指向新路径。
//   - 无法识别的目录 → 打 warning 跳过, 不动用户数据。
func MigrateLegacyDirs() error {
	entries, err := os.ReadDir(paths.VersionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录还没建, 无需迁移
		}
		return fmt.Errorf("扫描 versions 目录失败: %w", err)
	}

	currentOldName := ""
	if t := ReadTarget(); t != "" {
		currentOldName = filepath.Base(t)
	}

	var migrated bool
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if newDirNameRe.MatchString(name) {
			continue // 已是新规范 (纯 semver, 旧的无前缀目录)
		}
		newName := legacyToNewName(name)
		if newName == "" {
			// 非遗留目录。可能是新的 {distro}-{version} 命名 —— 各发行版版本号格式不一
			// (Temurin 用 21.0.5+11, Corretto 用 21.0.12.8.1), 不能用固定正则判断。
			// 这里用 MajorOf: 只要 distro 前缀后能解析出合法大版本号, 就视为有效目录跳过。
			// 注意必须放在 legacyToNewName 之后 —— SplitDistro 会把遗留的 jdk-21.0.12+8
			// 拆出 "jdk" 前缀, 若先判这里会误跳过该迁移的目录。
			if MajorOf(name) > 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "⚠️  跳过无法识别的目录: %s (不是已知的 JDK 版本目录)\n", name)
			continue
		}
		if newName == name {
			continue
		}

		oldPath := filepath.Join(paths.VersionsDir, name)
		newPath := filepath.Join(paths.VersionsDir, newName)

		// 新名已存在: 不覆盖 (用户可能已装新名版本)
		if _, err := os.Stat(newPath); err == nil {
			fmt.Fprintf(os.Stderr, "⚠️  迁移跳过 %s → %s (目标已存在)\n", name, newName)
			continue
		}

		// current 链接若指向旧目录, 先解除, rename 后重建
		relinkCurrent := name == currentOldName
		if relinkCurrent {
			if err := Remove(paths.CurrentLink); err != nil {
				return fmt.Errorf("迁移时解除 current 失败: %w", err)
			}
		}

		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("重命名 %s → %s 失败: %w", name, newName, err)
		}
		migrated = true
		fmt.Fprintf(os.Stderr, "📦 迁移版本目录: %s → %s\n", name, newName)

		if relinkCurrent {
			if err := Create(paths.CurrentLink, newPath); err != nil {
				return fmt.Errorf("迁移后重建 current 失败: %w", err)
			}
		}
	}

	if migrated {
		fmt.Fprintln(os.Stderr, "✅ 旧版本目录已迁移到新命名格式")
	}
	return nil
}

// SplitDistro 把本地版本目录名拆成 (distro, version)。
// 目录命名格式: {distro}-{version}, 如 "temurin-21.0.5+11" → ("temurin", "21.0.5+11")。
//
// 规则:
//   - 形如 "{字母前缀}-{数字开头...}" → 拆出前缀作 distro, 剩余作 version。
//   - 无 distro 前缀 (旧目录 "21.0.12+8" / "jdk-21.0.12+8" / 纯版本号输入) →
//     distro 返回 app.DefaultDistro ("temurin"), version 返回原串。
//
// 拆分规则: 从左往右贪心吸收"全字母段", 直到某段后面跟的不是数字开头的内容。
// 版本号必以数字开头, 故 "temurin-ea-28+14-ea-beta" 拆成 ("temurin-ea",
// "28+14-ea-beta"), "graalvm-ce-25.3.4.1" 拆成 ("graalvm-ce", "25.3.4.1")
// —— distro 名可以带 "-" (temurin-ea), 而版本号里的字母段 (ea-beta) 不会被
// 误并入 distro (其后的段不以数字开头)。
// 这样 "jdk-21.0.12+8" 的 "jdk" 不会被当成 distro ("jdk" 不是合法发行版,
// 会被拆成 ("jdk", "21.0.12+8") —— 但调用方按 app.DefaultDistro 兜底, 不影响)。
//
// 纯函数, 便于表驱动测试。
func SplitDistro(name string) (distro, version string) {
	s := strings.TrimSpace(name)
	if s == "" {
		return app.DefaultDistro, ""
	}
	// 找第一个 "-"
	dash := strings.IndexByte(s, '-')
	if dash <= 0 {
		// 无 "-" 或以 "-" 开头: 无 distro 前缀
		return app.DefaultDistro, s
	}
	prefix := s[:dash]
	rest := s[dash+1:]
	// 前缀必须全是字母 (distro 名约定: temurin/corretto/microsoft), 且 rest 非空
	if prefix == "" || rest == "" {
		return app.DefaultDistro, s
	}
	if !isAllLetters(prefix) {
		return app.DefaultDistro, s // 前缀含非字母, 视为无 distro
	}
	distro, version = prefix, rest
	// 多段 distro 名 (如 temurin-ea): rest 又以 "全字母段-" 开头且其后跟数字
	// (版本号必以数字开头) 时, 把该字母段并入 distro, 循环处理任意段数。
	for {
		d := strings.IndexByte(version, '-')
		if d <= 0 {
			break
		}
		seg, after := version[:d], version[d+1:]
		if seg == "" || after == "" || !isAllLetters(seg) {
			break
		}
		if after[0] < '0' || after[0] > '9' {
			break
		}
		distro = distro + "-" + seg
		version = after
	}
	return distro, version
}

// isAllLetters 判断 s 非空且全为 ASCII 字母。
func isAllLetters(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// MajorOf 从目录名 / 版本串里解析大版本号, 解析失败返回 0。
// 目录名采用 {distro}-{version} 形式 ("temurin-21.0.12+8" → 21); 同时容错
// 旧的无前缀目录 ("21.0.12+8" → 21, 启动时视为 temurin) 和带 jdk- / jdk
// 前缀的历史输入 (迁移期的用户手输 / 残留旧目录)。
//
// 先用 SplitDistro 剥掉 distro 前缀 (若有), 再取开头连续数字。
// 导出以供 doctor 包复用 (检查版本目录有效性), 避免解析逻辑重复。
// 纯函数, 便于表驱动测试。
func MajorOf(s string) int {
	_, ver := SplitDistro(strings.TrimSpace(s))
	ver = strings.TrimPrefix(ver, "jdk-")
	ver = strings.TrimPrefix(ver, "jdk")
	ver = strings.TrimPrefix(ver, "JDK-")
	ver = strings.TrimPrefix(ver, "JDK")
	// 取开头连续数字作为大版本号
	end := strings.IndexFunc(ver, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(ver)
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(ver[:end])
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

// stripPrefix 去掉版本串开头的 distro- / jdk- / jdk / JDK- / JDK 前缀,
// 便于精确比较。让 "21.0.12+8" 能同时匹配 "temurin-21.0.12+8" 和
// "jdk-21.0.12+8"。纯函数, 便于测试。
func stripPrefix(s string) string {
	_, s = SplitDistro(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "jdk-")
	s = strings.TrimPrefix(s, "jdk")
	s = strings.TrimPrefix(s, "JDK-")
	s = strings.TrimPrefix(s, "JDK")
	return s
}

// versionParts 把版本目录名解析成数字段切片, 用于语义版本比较。
// 目录名采用 {distro}-{version} 形式 "temurin-21.0.12+8" → [21, 0, 12, 8];
// 同时容错旧的无前缀目录 ("21.0.12+8") 和带 jdk- / jdk 前缀的历史输入。
//
// 先用 SplitDistro 剥掉 distro 前缀 (否则 distro 字母段会被 Atoi 成 0, 破坏比较)。
// 纯函数, 便于表驱动测试。
func versionParts(s string) []int {
	_, s = SplitDistro(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "jdk-")
	s = strings.TrimPrefix(s, "jdk")
	s = strings.TrimPrefix(s, "JDK-")
	s = strings.TrimPrefix(s, "JDK")
	var parts []int
	for _, seg := range strings.FieldsFunc(s, func(r rune) bool {
		return r < '0' || r > '9' // 用 "非数字" 作分隔符: . + 都会拆开
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
// 例如 "21.0.12+8" 不小于 "21.0.5+11" (12 > 5); "8.0.502+7" 大于 "8.0.402+6"。
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
