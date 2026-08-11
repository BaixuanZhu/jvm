# AGENTS.md

## Project overview

`jvm-site` 是 [jvm](https://github.com/BaixuanZhu/jvm)（Windows 上的 Java 版本管理器）的**官网**，部署在 GitHub Pages：<https://baixuanzhu.github.io/jvm/>。本目录是该仓库 `gh-pages` 分支的 **git worktree**——主仓库在 `D:\code\jvm`（`main` 分支是 Go 工具本体），两个 worktree 各管各的分支。

纯静态站点，用 **GitHub Pages 原生 Jekyll** 构建（无主题、自定义布局），无前端框架、无打包步骤。内容是面向用户的中文文档：首页（Hero + 特性 + 发行版卡片）、安装 / 命令 / 发行版 / 配置 / FAQ 五个文档页、更新日志。

## Setup commands

无 `Gemfile`——**构建完全由 GitHub Pages 服务端 Jekyll 完成**。工作流：编辑 Markdown → push `gh-pages` → GitHub 自动构建（~30s）→ 线上生效。

本地预览（可选，需本机自备 Ruby + Jekyll）：

```bash
cd D:/code/jvm-site
jekyll serve --baseurl /jvm      # 访问 http://127.0.0.1:4000/jvm/
```

发布 = push 到 `gh-pages` 分支。无需打 tag、无需 CI workflow，Pages 直连该分支。

## Code style

- **文案全中文**；事实信息（发行版清单、ARM64 支持矩阵、校验算法、命令语法、版本号写法）必须与主仓库 `CHANGELOG.md` / Release 保持一致。
- 每页 Markdown 顶部 YAML front matter：`title`（必填）+ `description`（SEO，一句话）。`layout` 省略时按 `_config.yml` 的 `defaults` 取 `docs`；`index.md` 显式用 `landing`，`changelog.md` 显式用 `docs`。
- 站内链接**必须**用 `{{ '/docs/xxx/' | relative_url }}`（受 `baseurl: "/jvm"` 影响，写死路径会断）；外链加 `target="_blank" rel="noopener"`。
- CSS 手写在 `assets/css/`（`main.css` 主样式 + `syntax.css` Rouge token 配色）；JS 是原生 ES5（`assets/js/main.js`），无转译、无依赖。
- 静态资源破缓存靠 URL 后缀 `?v={{ site.time | date: '%Y%m%d%H%M' }}`，每次构建自动变化。

## Testing instructions

无自动化测试，全靠人工 / 抓取验证：

1. **构建状态**：`gh api repos/BaixuanZhu/jvm/pages/builds/latest --jq .status`（`built` 即成功；失败时 `.error.message` 给原因，多为 Liquid 语法 / front matter 错误）。
2. **线上内容**：push 后用 WebFetch 抓对应 URL 核对关键文案（标题、表格行、示例命令）。
3. **本地预览**：`jekyll serve` 后走查页面布局与移动端导航。

## Architecture

```
index.md                 首页（layout: landing）—— Hero + 特性网格 + 发行版卡片 + CTA
404.md                   站内风格 404
changelog.md             更新日志（layout: docs）—— 与主仓库 CHANGELOG.md 同步
docs/                    文档页（layout 由 defaults 默认 docs）
  install.md  commands.md  distros.md  config.md  faq.md
_config.yml              Jekyll 配置：baseurl/url、permalink: pretty、
                         plugins（seo-tag + sitemap）、exclude、defaults（docs/ → docs 布局）
_layouts/
  landing.html           首页骨架（head + header + content + footer + main.js）
  docs.html              文档页骨架（左侧硬编码侧边栏 + 右侧正文）
_includes/
  header.html            顶栏（品牌 + 导航 + 下载按钮 + 移动端汉堡菜单），两套布局共享
  footer.html            页脚（链接 + 版权）
assets/
  css/main.css           全站样式（:root 变量 / 特性卡 / 发行版网格 / 终端动画）
  css/syntax.css         Rouge 代码高亮 token 配色
  js/main.js             复制按钮 / 代码块顶栏包装 / 移动端导航 /
                         IntersectionObserver 滚动入场 / 首页终端打字动画
```

布局约定：`landing.html` 与 `docs.html` 共享 head 结构，靠 `body` class（`landing` / `docs-page`）切样式；侧边栏导航**硬编码**在 `docs.html`，新增 doc 页须在此补一行链接。

## Hard constraints

- **worktree 关系**：本目录是 `D:\code\jvm` 的 `gh-pages` 分支 worktree，不能在主 worktree `git checkout gh-pages`（已被本目录占用）。改主仓库代码请切到 `D:\code\jvm`。
- **内容是工具的镜像**：改主仓库工具行为（新发行版、新命令、ARM64 支持变化）后，**必须同步更新本站**——重点是 `docs/distros.md` 的表格与 `changelog.md` 的新版本段。
- **baseurl**：所有站内链接 / 资源引用走 `relative_url` 过滤器；禁止写死 `/jvm/...` 或根绝对路径。
- **无主题、无构建链**：禁止引入 jekyll theme gem 或 npm / 打包工具；JS 保持原生 ES5 单文件。
- **AGENTS.md 不渲染**：本文件已在 `_config.yml` 的 `exclude` 里，Jekyll 不会把它当页面输出（避免出现在线上 / sitemap）。根目录新增非页面文件同理需加进 exclude。
- **permalink: pretty**：URL 形如 `/docs/install/`；新增页面的 `title` 会进入侧边栏与 `<title>`。

## Known gotchas

- 发行版卡片网格 `.distro-grid` 用 `repeat(auto-fit, minmax(280px, 1fr))` 自适应，增减卡片无需动 CSS。
- 改顶栏 / 页脚只动 `_includes/` 里对应文件，两套布局自动跟着变，无需各改一份。
- `_config.yml` 的 `exclude` 已含 `README.md` / `Gemfile` / `Gemfile.lock` / `AGENTS.md`——这些根目录文件不会上线。
- 改 `_config.yml` 后 GitHub Pages 构建可能延迟更久；改完优先盯一次 `pages/builds/latest` 状态。
