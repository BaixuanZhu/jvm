// 本文件实现版本固定文件 (.jvmrc 及生态兼容格式, 见 internal/pinrc) 的
// 消费侧共享逻辑: 无参 use / exec 与自动切换三条链路共用的 spec 获取与
// 按来源分流的宽松版本匹配。
package cmd

import (
	"fmt"
	"os"
	"strings"

	"jvm/internal/app"
	"jvm/internal/junction"
	"jvm/internal/pinrc"
)

// versionFromPinrc 从当前目录向上查找版本固定文件并返回归一化 spec,
// 供 Use 无参数时调用。找不到时直接 Fail 并给出友好提示。
//
// 第二返回值 loose 表示来源是外来格式 (.java-version / .tool-versions /
// .sdkmanrc): 这些生态普遍写不带 build 号的半截版本号, 匹配已装目录时
// 走宽松规则 (见 resolveVersionLoose); .jvmrc 保持 jvm 原生的严格语义。
func versionFromPinrc() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		app.Fail("获取当前目录失败: " + err.Error())
	}
	spec, src, foundPath, found := pinrc.FindUp(cwd)
	if !found {
		app.Fail("用法: jvm use <[distro@]版本号>\n" +
			"  或在当前目录 (或上层) 创建 .jvmrc: 运行 jvm pin <版本号>\n" +
			"  (也兼容读取 .java-version / .tool-versions / .sdkmanrc)")
	}
	fmt.Printf("📌 读取 %s: %s\n", foundPath, spec)
	return spec, src != pinrc.SrcJVMRC
}

// resolveVersionLoose 宽松匹配已装版本: 先按 spec 原样解析 (与显式命令
// 参数同一规则), 失败且版本号非纯大版本时降级为大版本取该组语义最新。
// 仅用于外来 rc 格式 —— sdkman/asdf 生态的版本号普遍不含 build 号,
// 严格匹配几乎必然落空; .jvmrc 与显式命令参数保持严格语义 (半截版本号
// 报错, 见 junction.ResolveVersion 的规则说明)。
func resolveVersionLoose(spec app.VersionSpec) (string, error) {
	dir, origErr := junction.ResolveVersion(spec.Distro, spec.Version)
	if origErr == nil {
		return dir, nil
	}
	major := junction.MajorOf(spec.Version)
	if major > 0 && itoa(major) != strings.TrimSpace(spec.Version) {
		if dir, err := junction.ResolveVersion(spec.Distro, itoa(major)); err == nil {
			return dir, nil
		}
	}
	return "", origErr
}
