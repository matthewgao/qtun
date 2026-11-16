# ProxyBar

macOS 菜单栏快速代理切换工具。点菜单栏图标弹出下拉菜单，在「直连 / 各代理配置」之间一键切换系统全局代理；设置窗口管理配置（增删改、拖拽排序）。

通用工具：qtun、Clash 等只是普通配置条目，不控制任何代理进程本身。

## 要求

- macOS 14+（开发机验证于 macOS 26.7.2 / Intel）
- Xcode 或 Command Line Tools（Swift 6.0+）
- 无需 root：读写系统代理走 `/usr/sbin/networksetup`（普通用户即可）

## 构建与安装

```bash
cd tools/proxybar
make test        # 单元测试（纯逻辑核心，50 例）
make app         # release 构建 + 组装 build/ProxyBar.app + ad-hoc 签名
make run         # 运行 build/ProxyBar.app（开发用）
make install     # 安装到 /Applications（日常使用推荐）
```

通用二进制：`UNIVERSAL=1 make app`。

**开机启动**：请先 `make install` 再在设置里开启「登录时启动」（SMAppService 要求稳定的 app 路径；从 build/ 注册会被 clean 掉）。重新安装后需重新开关一次。

## 使用

菜单栏图标：`network` = 有代理生效；`network.slash` = 直连。

下拉菜单：

```
✓ 直连
─────────────
  导入的配置        ← 你的各配置，✓ 标记当前生效者
  Clash
  自定义（未匹配任何配置）  ← 仅当系统状态不匹配任何配置时出现
─────────────
  导入当前系统配置…
  刷新状态
  设置…
─────────────
  退出 ProxyBar
```

- **配置模型**：名称 + 可选 HTTP / HTTPS / SOCKS（host:port）+ 可选 PAC URL + 绕过域名列表。一个配置可同时启用多种类型（Clash 混合端口用编辑器里的「填充三端点」一键填好）。
- **应用范围**：默认所有启用的物理网络服务（排除 iBridge 与 bridge* 设备；本机即 Wi-Fi）。设置 ▸ 通用里可勾选覆盖。
- **首次运行**：自动把当前系统代理状态导入为一条配置（例如 qtun 正在用的 PAC）。
- **外部改动**：qtun 启停、系统设置手改都会被实时感知（SCDynamicStore 通知 + 防抖），菜单只重新标注状态，**绝不会自动改回**——与 qtun 的自动 PAC 和平共存。

## 匹配语义（v1）

- 配置声明的类型必须开启且 host（忽略大小写/空白）与 port 一致；未声明的类型必须关闭（关闭状态下残留的 host/port 值被忽略）。
- PAC URL trim 后精确相等。
- **绕过域名列表不参与匹配**：bypass 不同但其余一致仍显示 ✓。
- 所有目标服务状态一致才选中某配置；混合态显示「自定义」。

## 调试 CLI

无需 UI 即可驱动真实应用路径（E2E/脚本化）：

```bash
/Applications/ProxyBar.app/Contents/MacOS/ProxyBar --cli-status        # 打印当前选中态与各服务状态 JSON
/Applications/ProxyBar.app/Contents/MacOS/ProxyBar --cli-direct        # 切直连
/Applications/ProxyBar.app/Contents/MacOS/ProxyBar --cli-apply Clash   # 按名称应用配置
```

## 已知边界

- 不支持认证代理（用户名/密码）。
- 变更通知键只镜像主服务；非主服务的外部改动靠「刷新状态」/应用后刷新兜底。
- 不做按网络位置（Location）的配置。
- 配置文件：`~/Library/Application Support/com.gaoshen.proxybar/store.json`（可手改；损坏时自动备份为 `store.json.corrupt-<时间戳>` 并从空开始）。

## 架构

```
Sources/ProxyBar/
├── Models/      纯数据 + 匹配器（ProfileMatcher）
├── Services/    NetworkSetup（纯命令构造/解析）、Shell（Process 边界）、
│                ServiceEnumerator、ProxyStateReader、ProxyApplier（actor 串行）、
│                ChangeWatcher（SCDynamicStore）、ProfileStore（JSON）、LaunchAtLogin
├── ViewModels/  AppModel（@MainActor @Observable 根模型）
└── Views/       MenuContent / SettingsView / ProfileEditorView
```

所有 networksetup I/O 藏在 `CommandRunning` 协议后：单测用 FakeRunner 注入，日后可换 SystemConfiguration 后端。应用后总是重读真实状态推导勾选，绝不做乐观假设。

与 qtun 主程序完全解耦：本目录无 .go 文件，仓库根 `go build ./...` 不受影响。