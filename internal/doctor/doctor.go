// Package doctor 实现 jvm doctor 命令: 全面诊断环境配置是否健康。
//
// 检查项 (每项输出 ✓ 通过 / ✗ 有问题, ✗ 附修复建议):
//   - 目录结构: ~/.jvm 和 ~/.jvm/versions 是否存在
//   - 配置文件: config.toml 存在时是否可解析 (Load 的启动警告用户未必注意到)
//   - junction: current 链接是否存在且指向真实存在的版本目录
//   - JAVA_HOME: 注册表持久化值是否指向 ~/.jvm/current
//   - PATH 冲突: 是否有别的 java.exe 出现在 ~/.jvm/current/bin 之前 (会抢先)
//   - shell 集成: PowerShell 5.x/7+ 和 bash 的 profile 是否已注入
//   - current 的 java: ~/.jvm/current/bin/java.exe 是否存在
//   - current 的 java 版本: 实跑 java -version 是否成功 (排除损坏二进制)
//   - 版本目录完整性: ~/.jvm/versions/ 下各目录是否都有 bin/java.exe
//   - 临时目录残留: ~/.jvm 下的 .tmp-extract-* 解压半成品 (中断安装遗留)
//   - 用户 PATH: 注册表用户 PATH 是否包含 current/bin (保证新终端能找到 java)
//   - 注册表 PATH 残留: 用户 PATH 里是否有非 current/bin 的旧 JDK 路径
//   - 下载缓存: 缓存占用汇报 (信息性, 恒通过 —— 缓存与 .part 续传分片是
//     设计行为, 删不删交给用户)
//
// 检查函数都接收显式参数 (路径/已读好的环境值), 不直接读全局状态 ——
// 这样可以用临时目录和注入值做表驱动测试, 不污染真实注册表/profile。
// Run() 负责从 paths/env/shell 取真实状态再分发给检查函数。
//
// doctor --fix 在报告后对失败项执行自动修复 (见 applyFixes): 只修无争议项
// (目录/JAVA_HOME/junction 重建/profile 注入/PATH 补全/残留清理/解压半成品
// 清理), 残留清理前逐条确认; 需重装或动系统 PATH 的项保留建议不动。
package doctor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jvm/internal/config"
	"jvm/internal/env"
	"jvm/internal/junction"
	"jvm/internal/paths"
	"jvm/internal/shell"
)

// check 是单个检查项的结果。
type check struct {
	ok     bool   // true=通过, false=有问题
	name   string // 检查项名称
	detail string // 通过时是说明, 失败时是问题描述
	fix    string // 失败时的修复建议 (通过时为空)
}

// profileItem 是一个 shell profile 的检查输入。
type profileItem struct {
	label string // 给用户看的名称 (如 "PowerShell 5.x")
	path  string // profile 文件路径
}

// Run 执行全部检查并打印诊断报告。fix 为 true 时 (--fix) 对失败项执行自动修复,
// assumeYes 为 true 时 (-y) 跳过残留清理的逐条确认 (供脚本调用)。
// installDir 是 config.toml 的 install_dir 值 (空 = 未配置), 用于检测
// 数据目录重定向后旧默认目录的残留版本 (只提示, 不自动搬迁)。
func Run(fix, assumeYes bool, installDir string) {
	fmt.Println("🏥 jvm 环境诊断")
	fmt.Println(strings.Repeat("─", 40))

	// 从真实全局状态读取检查所需输入
	javaHome, _ := env.ReadUserEnv("JAVA_HOME")
	regPath, _ := env.ReadUserEnv("PATH")
	profiles := []profileItem{
		{"PowerShell 5.x", shell.PsProfilePath()},
		{"PowerShell 7+", shell.Ps7ProfilePath()},
		{"bash", shell.BashProfilePath()},
	}
	javaBin := filepath.Join(paths.CurrentLink, "bin", "java.exe")

	// install_dir 重定向后, 旧默认目录 {Root}/versions 若仍有版本则提示搬迁
	legacyVersions := ""
	if installDir != "" {
		legacyVersions = filepath.Join(paths.Root, "versions")
	}

	checks := []check{
		checkDirs(paths.Root, paths.VersionsDir, legacyVersions),
		checkConfig(paths.Root),
		checkJunction(paths.CurrentLink),
		checkJavaHome(javaHome, paths.CurrentLink),
		checkPathConflict(os.Getenv("PATH"), paths.CurrentLink),
		checkShellIntegration(profiles),
		checkCompletion(profiles),
		checkCurrentJava(paths.CurrentLink),
		checkJavaVersion(javaBin, runJavaVersion),
		checkVersionsIntegrity(paths.VersionsDir),
		checkTmpResidue(paths.Root),
		checkUserPathCurrent(regPath, paths.CurrentLink),
		checkRegistryPathResidue(regPath, paths.CurrentLink),
		checkCache(paths.CacheDir),
	}

	var failed []check
	for _, c := range checks {
		printCheck(c)
		if !c.ok {
			failed = append(failed, c)
		}
	}

	fmt.Println(strings.Repeat("─", 40))
	switch {
	case len(failed) == 0:
		fmt.Println("✅ 所有检查通过, 环境配置正常。")
	case fix:
		applyFixes(failed, assumeYes, regPath)
	default:
		fmt.Printf("⚠️  发现 %d 个问题 (见上方 ✗ 项的修复建议)。\n", len(failed))
		fmt.Println("运行 jvm doctor --fix 可自动修复部分问题。")
	}
}

// printCheck 打印单个检查项结果
func printCheck(c check) {
	mark := "✓"
	if !c.ok {
		mark = "✗"
	}
	fmt.Printf("%s %s: %s\n", mark, c.name, c.detail)
	if c.fix != "" {
		fmt.Printf("    修复: %s\n", c.fix)
	}
}

// checkDirs 检查 root 和 versions 目录是否存在。legacyVersions 非空表示
// install_dir 重定向过, 此时旧默认目录若仍有已装版本, 附带搬迁提示 (信息性,
// 不算失败 —— 搬迁需跨目录移动大量数据, 只交给用户手动做)。
func checkDirs(root, versionsDir, legacyVersions string) check {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return check{
			ok:     false,
			name:   "目录结构",
			detail: fmt.Sprintf("~/.jvm (%s) 不存在", root),
			fix:    "运行一次任意 jvm 命令 (如 jvm version) 会自动创建",
		}
	}
	if info, err := os.Stat(versionsDir); err != nil || !info.IsDir() {
		return check{
			ok:     false,
			name:   "目录结构",
			detail: fmt.Sprintf("版本目录 (%s) 不存在", versionsDir),
			fix:    "运行 jvm install <版本号> 安装一个 JDK",
		}
	}
	detail := "~/.jvm 和 versions 目录就绪"
	if legacyVersions != "" && legacyVersions != versionsDir {
		if n := countSubdirs(legacyVersions); n > 0 {
			detail = fmt.Sprintf("~/.jvm 和 versions 目录就绪 (提示: 旧默认目录 %s 仍有 %d 个版本, 可手动搬迁到 %s)", legacyVersions, n, versionsDir)
		}
	}
	return check{ok: true, name: "目录结构", detail: detail}
}

// countSubdirs 统计目录下的子目录数 (不存在返回 0)。
func countSubdirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// checkConfig 检查 config.toml 是否可解析。缺失视为正常 (未使用自定义配置);
// 存在但语法非法时, 启动时 Load 只在 stderr 警告一次, 用户未必注意到,
// 这里显式暴露 (此时全部配置回退默认, mirror/arch 等自定义项静默失效)。
func checkConfig(root string) check {
	path := filepath.Join(root, "config.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return check{ok: true, name: "配置文件", detail: "未使用自定义配置 (全部默认值)"}
	}
	if err := config.ValidateFile(path); err != nil {
		return check{
			ok:     false,
			name:   "配置文件",
			detail: fmt.Sprintf("config.toml 解析失败 (当前全部回退默认值): %v", err),
			fix:    "修正 TOML 语法, 或删除该文件回退全部默认值",
		}
	}
	return check{ok: true, name: "配置文件", detail: "config.toml 可解析"}
}

// checkTmpResidue 检查 jvm 根目录下的解压临时目录残留 (.tmp-extract-*,
// 安装/升级时解压中断留下的半成品, 白占磁盘且不会自愈)。
func checkTmpResidue(root string) check {
	dirs := TmpResidueDirs(root)
	if len(dirs) == 0 {
		return check{ok: true, name: "临时目录残留", detail: "无 .tmp-extract-* 解压半成品"}
	}
	return check{
		ok:     false,
		name:   "临时目录残留",
		detail: fmt.Sprintf("%d 个解压半成品目录残留 (安装中断遗留)", len(dirs)),
		fix:    "jvm doctor --fix 自动清理, 或手动删除 .tmp-extract-* 目录",
	}
}

// TmpResidueDirs 列出 root 下全部 .tmp-extract-* 目录的完整路径。
// 诊断 (checkTmpResidue) 与 --fix 修复同源, 保证判定与清理看的是同一份清单。
// 目录不存在或无残留返回 nil。
func TmpResidueDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".tmp-extract-") {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs
}

// cacheSuggestBytes 是缓存占用的建议清理阈值 (超过时 detail 附清理提示)。
const cacheSuggestBytes = 1 << 30 // 1 GB

// checkCache 汇报下载缓存占用 (信息性检查, 恒通过 —— 缓存 zip 是设计行为,
// .part 分片是断点续传资源, 都不是故障, 删不删交给用户)。
func checkCache(cacheDir string) check {
	zips, parts, total := cacheStats(cacheDir)
	detail := fmt.Sprintf("%d 个安装包 zip 共 %.1f MB", zips, float64(total)/1024/1024)
	if parts > 0 {
		detail += fmt.Sprintf(", %d 个未完成分片 (.part)", parts)
	}
	if total >= cacheSuggestBytes || parts > 0 {
		detail += ", 可 jvm cache clean 释放"
	}
	return check{ok: true, name: "下载缓存", detail: detail}
}

// cacheStats 统计缓存目录的 zip 数量、.part 分片数与 zip 总大小
// (目录不存在视为空)。
func cacheStats(cacheDir string) (zips, parts int, total int64) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0, 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(e.Name(), ".zip"):
			zips++
			total += info.Size()
		case strings.HasSuffix(e.Name(), ".zip.part"):
			parts++
		}
	}
	return zips, parts, total
}

// checkJunction 检查 current 链接是否有效。
//
// 判断"是否是链接"用 os.Readlink 是否成功为准 —— 不能用 os.Lstat 的 ModeSymlink 位:
// Windows junction (IO_REPARSE_TAG_MOUNT_POINT) 在 Go 里 Lstat 不设置 ModeSymlink
// (那是给真 symlink IO_REPARSE_TAG_SYMLINK 的), 而是设置 ModeIrregular。
// 但 os.Readlink 对 junction 和 symlink 都能正确解析 (Go 1.20+)。
func checkJunction(link string) check {
	if _, err := os.Lstat(link); err != nil {
		return check{
			ok:     false,
			name:   "current 链接",
			detail: "尚未选定任何版本 (current 不存在)",
			fix:    "运行 jvm use <版本号> 选择一个版本",
		}
	}
	target, err := os.Readlink(link)
	if err != nil || target == "" {
		// current 存在但 Readlink 失败 → 是普通目录而非链接 (旧版残留或手动建的)
		return check{
			ok:     false,
			name:   "current 链接",
			detail: "current 不是链接 (可能是普通目录)",
			fix:    "删除后重新 jvm use <版本号>",
		}
	}
	// 验证目标真实存在 (指向已被删除的版本目录 = 悬空链接)
	if _, err := os.Stat(link); err != nil {
		return check{
			ok:     false,
			name:   "current 链接",
			detail: fmt.Sprintf("current 指向的目标不存在 (%s)", filepath.Base(target)),
			fix:    "版本目录可能被手动删除, jvm use <版本号> 重新指向有效版本",
		}
	}
	return check{ok: true, name: "current 链接", detail: fmt.Sprintf("指向 %s", filepath.Base(target))}
}

// checkJavaHome 检查持久化的 JAVA_HOME 是否指向 current。
// javaHome 是已从注册表读出的值 (由 Run 读取后注入, 便于测试)。
func checkJavaHome(javaHome, currentLink string) check {
	if strings.TrimSpace(javaHome) == "" {
		return check{
			ok:     false,
			name:   "JAVA_HOME",
			detail: "注册表 HKCU\\Environment 未设置 JAVA_HOME",
			fix:    "运行 jvm use <版本号> 会自动设置",
		}
	}
	if !strings.EqualFold(filepath.Clean(javaHome), filepath.Clean(currentLink)) {
		return check{
			ok:     false,
			name:   "JAVA_HOME",
			detail: fmt.Sprintf("JAVA_HOME=%s (期望 %s)", javaHome, currentLink),
			fix:    "运行 jvm use <版本号> 会自动修正",
		}
	}
	return check{ok: true, name: "JAVA_HOME", detail: "指向 ~/.jvm/current"}
}

// checkPathConflict 检查 PATH 里是否有别的 java.exe 在 current/bin 之前。
// pathEnv 是已读好的 PATH 值 (便于测试), currentLink 是 current 目录路径。
func checkPathConflict(pathEnv, currentLink string) check {
	binPath := filepath.Join(currentLink, "bin")
	if pathEnv == "" {
		return check{ok: true, name: "PATH 冲突", detail: "PATH 为空, 无冲突"}
	}
	for _, entry := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// 先遇到 current/bin, 之后有无冲突都不影响 (current 优先)
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(binPath)) {
			return check{ok: true, name: "PATH 冲突", detail: "current/bin 在 PATH 中, 无抢先的 java"}
		}
		// 在遇到 current/bin 之前, 若某条目里有 java.exe, 就是抢先
		javaExe := filepath.Join(entry, "java.exe")
		if _, err := os.Stat(javaExe); err == nil {
			return check{
				ok:     false,
				name:   "PATH 冲突",
				detail: fmt.Sprintf("%s 里有 java.exe, 出现在 current/bin 之前", entry),
				fix:    "从 PATH 移除该目录, 或把它移到 current/bin 之后",
			}
		}
	}
	return check{ok: true, name: "PATH 冲突", detail: "PATH 中无抢先的 java"}
}

// checkShellIntegration 检查给定的 profile 列表是否已注入集成。
func checkShellIntegration(profiles []profileItem) check {
	var missing []string
	for _, p := range profiles {
		if !shell.ProfileHasIntegration(p.path) {
			missing = append(missing, p.label)
		}
	}
	if len(missing) == 0 {
		return check{ok: true, name: "shell 集成", detail: "PowerShell 5.x/7+ 和 bash profile 均已注入"}
	}
	return check{
		ok:     false,
		name:   "shell 集成",
		detail: fmt.Sprintf("未集成: %s", strings.Join(missing, ", ")),
		fix:    "重开终端自动补全, 或手动 jvm init <shell> --install",
	}
}

// checkCompletion 检查给定的 profile 列表是否已注入 Tab 补全块。
func checkCompletion(profiles []profileItem) check {
	var missing []string
	for _, p := range profiles {
		if !shell.CompletionHasIntegration(p.path) {
			missing = append(missing, p.label)
		}
	}
	if len(missing) == 0 {
		return check{ok: true, name: "Tab 补全", detail: "PowerShell 5.x/7+ 和 bash profile 均已注入"}
	}
	return check{
		ok:     false,
		name:   "Tab 补全",
		detail: fmt.Sprintf("未注入: %s", strings.Join(missing, ", ")),
		fix:    "重开终端自动补全, 或手动 jvm completion <shell> --install",
	}
}

// checkCurrentJava 检查 current/bin/java.exe 是否存在。
func checkCurrentJava(currentLink string) check {
	javaExe := filepath.Join(currentLink, "bin", "java.exe")
	if _, err := os.Stat(javaExe); err != nil {
		return check{
			ok:     false,
			name:   "current 的 java",
			detail: "current/bin/java.exe 不存在",
			fix:    "当前指向的版本可能损坏, 重新 jvm install + jvm use",
		}
	}
	return check{ok: true, name: "current 的 java", detail: "java.exe 就绪"}
}

// versionRunner 执行 java -version 并返回输出 (stdout+stderr 合并)。
// 拆成参数类型是为了让 checkJavaVersion 可注入假执行器做测试 ——
// 否则测试要么真跑 java 要么造假 exe, 都很别扭。
type versionRunner func(javaBin string) (output string, err error)

// runJavaVersion 是生产用的真实执行器: 5s 超时, 合并 stderr (java -version 走 stderr)。
func runJavaVersion(javaBin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, javaBin, "-version").CombinedOutput()
	return string(out), err
}

// checkJavaVersion 实跑 java -version, 排除 "文件存在但二进制损坏 / 缺 DLL" 的情况。
// run 参数是执行器 (注入, 便于测试)。javaBin 不存在视为跳过 (由 checkCurrentJava 负责)。
func checkJavaVersion(javaBin string, run versionRunner) check {
	if _, err := os.Stat(javaBin); err != nil {
		return check{ok: true, name: "java 版本", detail: "current 无 java.exe, 已由上文检查"}
	}
	output, err := run(javaBin)
	if err != nil {
		return check{
			ok:     false,
			name:   "java 版本",
			detail: fmt.Sprintf("执行 java -version 失败: %v", err),
			fix:    "该版本可能损坏或缺 DLL, 建议 jvm uninstall 后重装",
		}
	}
	// java -version 输出含 "version" 字样即认为正常
	if !strings.Contains(strings.ToLower(output), "version") {
		return check{
			ok:     false,
			name:   "java 版本",
			detail: fmt.Sprintf("java -version 输出异常: %s", strings.TrimSpace(output)),
			fix:    "该版本可能损坏, 建议 jvm uninstall 后重装",
		}
	}
	return check{ok: true, name: "java 版本", detail: "java -version 可正常执行"}
}

// checkVersionsIntegrity 检查 versions 目录下各版本子目录是否都有 bin/java.exe。
// 只检查能解析出大版本号的目录 (与 junction.ListLocal 一致), 跳过临时/无关目录。
func checkVersionsIntegrity(versionsDir string) check {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return check{ok: true, name: "版本目录完整性", detail: "versions 目录不存在或不可读, 已由上文检查"}
	}
	var broken []string
	total := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if junction.MajorOf(e.Name()) == 0 {
			continue // 非版本目录 (临时目录等), 跳过
		}
		total++
		javaExe := filepath.Join(versionsDir, e.Name(), "bin", "java.exe")
		if _, err := os.Stat(javaExe); err != nil {
			broken = append(broken, e.Name())
		}
	}
	if total == 0 {
		return check{ok: true, name: "版本目录完整性", detail: "无已安装版本"}
	}
	if len(broken) > 0 {
		return check{
			ok:     false,
			name:   "版本目录完整性",
			detail: fmt.Sprintf("缺少 bin/java.exe 的目录: %s", strings.Join(broken, ", ")),
			fix:    "这些可能是解压中断的半成品, 建议 jvm uninstall <目录名> 后重装",
		}
	}
	return check{ok: true, name: "版本目录完整性", detail: fmt.Sprintf("%d 个版本目录均完整", total)}
}

// checkUserPathCurrent 检查注册表持久化的用户 PATH 是否包含 current/bin。
// 区别于 checkPathConflict (查当前进程 PATH): 这里保证的是新开终端能找到 java。
// regPath 是已读好的注册表用户 PATH (由 Run 读取后注入, 便于测试)。
func checkUserPathCurrent(regPath, currentLink string) check {
	binPath := filepath.Join(currentLink, "bin")
	for _, entry := range strings.Split(regPath, string(os.PathListSeparator)) {
		entry = strings.TrimSpace(entry)
		if entry != "" && strings.EqualFold(filepath.Clean(entry), filepath.Clean(binPath)) {
			return check{ok: true, name: "用户 PATH", detail: "current/bin 已在用户 PATH"}
		}
	}
	return check{
		ok:     false,
		name:   "用户 PATH",
		detail: "用户 PATH 未包含 current/bin (新开终端可能找不到 java)",
		fix:    "jvm doctor --fix 自动补上, 或任意 jvm use 时自动设置",
	}
}

// ResidueEntries 返回用户 PATH 里的旧 JDK 残留条目: 含 java.exe 且不是
// current/bin 的路径。诊断 (checkRegistryPathResidue) 与修复 (applyFixes)
// 共用, 保证两者看到的是同一批条目。纯函数, 便于表驱动测试。
func ResidueEntries(regPath, currentLink string) []string {
	if strings.TrimSpace(regPath) == "" {
		return nil
	}
	binPath := filepath.Join(currentLink, "bin")
	var residue []string
	for _, entry := range strings.Split(regPath, string(os.PathListSeparator)) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// 跳过 current/bin (jvm 管理的)
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(binPath)) {
			continue
		}
		// 其余含 java.exe 的条目 = 旧 JDK 残留
		if _, err := os.Stat(filepath.Join(entry, "java.exe")); err == nil {
			residue = append(residue, entry)
		}
	}
	return residue
}

// checkRegistryPathResidue 检查注册表持久化的用户 PATH 里是否有非 current/bin 的旧 JDK 路径。
// 区别于 checkPathConflict (检查进程 PATH 的抢先冲突), 这里检查注册表里的残留:
// 即曾经手动加过的、未被 jvm 管理的 java.exe 路径, 它们会在新终端里造成版本混乱。
func checkRegistryPathResidue(regPath, currentLink string) check {
	if strings.TrimSpace(regPath) == "" {
		return check{ok: true, name: "注册表 PATH 残留", detail: "用户 PATH 未持久化, 无残留"}
	}
	residue := ResidueEntries(regPath, currentLink)
	if len(residue) == 0 {
		return check{ok: true, name: "注册表 PATH 残留", detail: "用户 PATH 无旧 JDK 残留"}
	}
	return check{
		ok:     false,
		name:   "注册表 PATH 残留",
		detail: fmt.Sprintf("PATH 里有旧 JDK: %s", strings.Join(residue, ", ")),
		fix:    "从用户环境变量 PATH 中移除旧 JDK 路径, 由 jvm use 统一管理 (doctor --fix 可代删)",
	}
}

// applyFixes 对失败的检查项执行自动修复 (--fix)。
// 只修无争议项: 目录/JAVA_HOME/junction 重建/profile 注入/用户 PATH 补全/残留清理;
// 残留清理会删用户 PATH 条目, 删除前逐条确认 (assumeYes 跳过);
// 需要重装或动系统 PATH 的项 (PATH 冲突/版本损坏) 保留建议不自动处理。
func applyFixes(failed []check, assumeYes bool, regPath string) {
	fmt.Println("🔧 自动修复:")
	fixed := 0
	for _, c := range failed {
		switch c.name {
		case "目录结构":
			if err := paths.EnsureDirs(); err != nil {
				fmt.Printf("  ✗ 目录结构: %v\n", err)
				continue
			}
			fmt.Println("  ✓ 目录结构: 已创建 ~/.jvm 与 versions 目录")
			fixed++
		case "current 链接":
			switch fixJunction() {
			case fixDone:
				fixed++
			}
		case "JAVA_HOME":
			if err := env.Persist("JAVA_HOME", paths.CurrentLink); err != nil {
				fmt.Printf("  ✗ JAVA_HOME: %v\n", err)
				continue
			}
			fmt.Printf("  ✓ JAVA_HOME: 已指向 %s\n", paths.CurrentLink)
			fixed++
		case "shell 集成", "Tab 补全":
			shell.EnsureIntegration()
			fmt.Println("  ✓ " + c.name + ": 已补全 profile 注入 (重开终端生效)")
			fixed++
		case "用户 PATH":
			if err := env.EnsureCurrentInPath(); err != nil {
				fmt.Printf("  ✗ 用户 PATH: %v\n", err)
				continue
			}
			fmt.Println("  ✓ 用户 PATH: 已把 current/bin 加入用户 PATH")
			fixed++
		case "注册表 PATH 残留":
			fixResidue(regPath, assumeYes)
			fixed++
		case "临时目录残留":
			// 纯临时目录无争议, 直接清 (extractAndPlace 每次也会先 RemoveAll,
			// 这里只是替用户把中断遗留的孤儿清掉)。
			dirs := TmpResidueDirs(paths.Root)
			removed := 0
			for _, d := range dirs {
				if err := os.RemoveAll(d); err != nil {
					fmt.Printf("  ✗ 临时目录残留: 删除 %s 失败: %v\n", d, err)
					continue
				}
				removed++
			}
			fmt.Printf("  ✓ 临时目录残留: 已清理 %d 个半成品目录\n", removed)
			fixed++
		default:
			// PATH 冲突 / current 的 java / java 版本 / 版本目录完整性: 需手动或重装
			fmt.Printf("  ⏭️  %s: 不自动修复 (见上方建议)\n", c.name)
		}
	}
	if fixed > 0 {
		fmt.Println("修复已执行, 重跑 jvm doctor 验证。")
	}
}

// fixResidue 清理用户 PATH 里的旧 JDK 残留: 逐条确认 (assumeYes=true 全部直接删),
// 被拒绝的条目保留。
func fixResidue(regPath string, assumeYes bool) {
	residue := ResidueEntries(regPath, paths.CurrentLink)
	if len(residue) == 0 {
		return // 环境已变化, 无残留可清
	}
	fmt.Println("  以下用户 PATH 条目含 java.exe 且不由 jvm 管理:")
	var remove []string
	for _, r := range residue {
		if assumeYes || confirmYes("    移除 "+r+" ?") {
			remove = append(remove, r)
		}
	}
	if len(remove) == 0 {
		fmt.Println("  ⏭️  注册表 PATH 残留: 未选择任何条目, 跳过")
		return
	}
	if err := env.RemoveFromUserPath(remove); err != nil {
		fmt.Printf("  ✗ 注册表 PATH 残留: %v\n", err)
		return
	}
	fmt.Printf("  ✓ 注册表 PATH 残留: 已移除 %d 条\n", len(remove))
}

// fixJunction 修复 current 链接: 缺失/悬空/空普通目录时重建到最新已装版本;
// current 是非空普通目录时跳过 (可能是用户数据, 不自动删)。输出结果行。
func fixJunction() fixState {
	switch junctionFixPlan(paths.CurrentLink) {
	case junctionSkip:
		fmt.Println("  ⏭️  current 链接: current 是非空普通目录, 需手动确认后删除 (不自动清用户数据)")
		return fixSkip
	case junctionReplace:
		if err := junction.Remove(paths.CurrentLink); err != nil {
			fmt.Printf("  ✗ current 链接: 移除旧链接失败: %v\n", err)
			return fixFail
		}
	}
	names, _ := junction.ListLocal()
	if len(names) == 0 {
		fmt.Println("  ⏭️  current 链接: 没有已安装版本可指向, 先 jvm install <版本号>")
		return fixFail
	}
	target := filepath.Join(paths.VersionsDir, names[0]) // ListLocal 降序, 首个即最新
	if err := junction.Create(paths.CurrentLink, target); err != nil {
		fmt.Printf("  ✗ current 链接: 重建失败: %v\n", err)
		return fixFail
	}
	fmt.Printf("  ✓ current 链接: 已重建 → %s\n", names[0])
	return fixDone
}

// fixState 是修复动作的执行状态。
type fixState int

const (
	fixDone fixState = iota // 修复成功
	fixFail                 // 修复失败
	fixSkip                 // 该项跳过自动修复
)

// junctionFixAction 是 current 链接问题的修复方式。
type junctionFixAction int

const (
	junctionCreate  junctionFixAction = iota // 链接不存在, 直接创建
	junctionReplace                          // 悬空链接/空普通目录, 删旧再建
	junctionSkip                             // 非空普通目录, 不自动处理
)

// junctionFixPlan 判断 current 链接问题的修复方式 (不做实际动作, 便于表测)。
func junctionFixPlan(link string) junctionFixAction {
	if _, err := os.Lstat(link); err != nil {
		return junctionCreate
	}
	if _, err := os.Readlink(link); err != nil {
		// 普通目录 (Readlink 失败): 空目录可删了重建, 非空不自动处理
		if dirIsEmpty(link) {
			return junctionReplace
		}
		return junctionSkip
	}
	return junctionReplace // 链接 (含悬空): 删了重建
}

// dirIsEmpty 判断目录是否为空 (不存在视为空)。
func dirIsEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}

// confirmYes 在终端问一个 y/N 问题, 回答 y/yes 返回 true。
func confirmYes(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}
