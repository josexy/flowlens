# Tasks

本变更已进入 apply。审查依据、F1–F4/D1–D2 分类和前序规范次序见 `design.md`，实现与验收结果见 `verification.md`。使用 go.mod 指定版本；Windows/Linux 测试设置 `CGO_ENABLED=0`，macOS 使用 `CGO_ENABLED=1`。每组视图改动后运行 `task vet:ui`。

## 1. 更新对照标准与差异文档

- [x] 1.1 更新 `docs/technical/ui-parity.md` 开头的强制 1.0.5 对齐规则，纳入本次逐模块结论、源码基线、原生优先准则与验证边界；保留 Tooltip inventory 及有用的历史记录。验证：`go test ./desktop/internal/parity` 通过，文档不再把所有差异统一描述为待恢复项。
- [x] 1.2 明确记录保持原生的编辑器、Hex、菜单、Help Slideover、侧栏折叠入口与内存图表无拖框缩放等差异；验证每条有两侧代码依据、当前取舍和验证状态，不把 D1 排成缩放实现任务，也不改写前序未完成任务。

## 2. 流量选择与详情入口

- [x] 2.1 在 `desktop/capture/{table,selection}.go` 去除左键选择结果的快照恢复/重写，保留最小右键定位；由现有原生选择状态驱动计数与焦点。以 Tester 替换旧点击语义用例，覆盖普通/重复点击、Ctrl/Cmd 增减、Shift 范围、原生拖选、快速拖动和失焦后继续键盘选择；验证 `go test ./desktop/capture ./desktop/store`。
- [x] 2.2 保留并验证排序后的 ID 选择、过滤/删除/淘汰后的焦点回落、切换历史清空、悬停阅读和右键批量操作；覆盖 `traffic-capture` 的对应场景，确认导出仍走既有 Exporter 且 ID 顺序/历史别名不变。验证 `go test ./desktop/capture ./desktop/logic`。
- [x] 2.3 在 `desktop/capture` 使用表格提交事件接入现有详情打开业务函数；在 `desktop/inspect` 增加关闭回调与可键盘激活的关闭按钮，在 live/history 两处接线。验证 Enter/无修饰键双击/Show、关闭保留选择并回落焦点、关闭后选择不重开、多选 Enter、输入框/模态框隔离、迟到 body 结果；运行 `go test ./desktop/capture ./desktop/inspect ./desktop/store`。
- [x] 2.4 更新两份 locale 的详情关闭标签、工具提示及 `docs/user-guide.md` 中单选/多选/范围/打开关闭说明；记录交互兼容性变化到版本说明的合适位置，不修改版本号。运行 `task i18n:generate`、`task i18n:check`、`task vet:ui`，验证说明不再要求 Ctrl/Cmd 打开详情或 Shift 单项切换。

## 3. 自绘导航的键盘入口

- [x] 3.1 将 Settings 导航和 MemStats 单项侧栏改为有真实焦点/键盘激活的基础控件，复用 `selectTab`/`Select`；以 Tester 验证 Tab/Shift+Tab、Enter/Space、可见焦点和草稿保留，运行 `go test ./desktop/settings ./desktop/memstats`。
- [x] 3.2 补齐 capture/history 行及 category 行/分组的键盘进入与激活，使用基础控件或列表选择/提交，不增加窗口级 key handler；验证折叠侧栏、切页、筛选后隐藏项不会响应，历史打开与分类过滤和点击一致，运行 `go test ./desktop/capture ./desktop/shell`。
- [x] 3.3 补齐 API Collection 的目录/请求主操作区键盘入口，隔离多选 checkbox、菜单及拖动柄；Tester 验证展开折叠、打开请求、选中一次、不误删除、不在拖动后额外打开，以及切页后焦点有效，运行 `go test ./desktop/collection ./desktop/store`。
- [x] 3.4 补齐 Rewrite 规则、Python 插件与文件树的主操作区键盘入口，保持开关、拖动、草稿缓存与原业务函数；验证键盘选择与点击结果一致且开关只执行一次。运行 `go test ./desktop/rewrite ./desktop/plugins ./desktop/store`，插件包使用真正可运行的 Python 3，不能以 skip 作为通过。
- [x] 3.5 在 `docs/user-guide.md` 增补这些导航项的键盘方式，按实际采用的 Tab stop 或列表方向键模型说明；运行 `task vet:ui` 和 `go test ./desktop/shortcuts`，验证没有并行的窗口快捷键入口且新标签保持双语一致。

## 4. 请求网格编辑焦点

- [x] 4.1 在 `desktop/widgets/editable_grid.go` 为名称和值单元格提供基础控件激活面，并保存稳定焦点身份及该次编辑原值；复用现有 Textarea，保留紧凑网格。Tester 验证不点击即可进入任意可见单元格、文本选中、长值展开、焦点可见与可视行构建上界，运行 `go test ./desktop/widgets`。
- [x] 4.2 实现 Enter/blur 提交、Tab/Shift+Tab 继续导航、Escape 恢复编辑前值，以及结束后的有效焦点回落；处理 composition Enter 和编辑中行/标签消失，避免回写邻行。验证 `request-workspace` 的全部编辑场景，运行 `go test ./desktop/widgets ./desktop/editor`。
- [x] 4.3 在 HTTP headers/query/cookies/urlencoded/form-data 与 WebSocket headers 调用方验证键盘编辑、重复字段顺序、禁用行、文件单元格和行内按钮隔离；仅在需要时调整 UI 身份映射，不改变持久化字段。更新用户指南的 Enter/Tab/Escape 说明；运行 `go test ./desktop/editor ./desktop/logic ./desktop/store` 与 `task vet:ui`。

## 5. 内存页可用宽度适配

- [x] 5.1 修改 `desktop/memstats/view.go` 的工具栏、摘要和指标区，使其按自身可用宽度换行/叠列；在现有 fixture 中增加窄内容区 Tester 用例，断言操作 bounds 位于可见区域且标签和值不重叠。验证主窗 1024×768 + 次侧栏 42% 的等效内容区、20%/15% 与宽窗口组合，运行 `go test ./desktop/memstats`。
- [x] 5.2 验证宽窄布局切换不重启采样、不清样本、不重置图例显隐，覆盖暂停/运行两种状态；增加浅深主题、英文/中文、配置最大字体的布局检查与可选 render 输出。更新差异文档说明原生图表与宽度适配；运行 `go test ./desktop/memstats ./desktop/store ./desktop/theme` 与 `task vet:ui`。

## 6. 集成验收

- [x] 6.1 在有效 Python 3 环境运行 `go test ./desktop/...`、`task vet:ui`、`task i18n:check` 和 `openspec validate align-native-ui-interactions --strict`，记录完整结果。若实现触及并发/生命周期，再运行 AGENTS.md 要求的 race 包；没有相关改动时不额外扩大网络/Worker 范围。
- [x] 6.2 在 Windows 实机以当前数据目录的副本验收 F1–F4：修饰键、拖选/右键、详情开关、导航键盘、中文 IME、窄内容区、浅深主题、字体/DPI；记录实际结果和限制，不以 headless 通过替代。macOS 的 Cmd 与 Linux/系统菜单验证接到前序仍未完成的实机矩阵，尚未执行者保持未完成。
- [x] 6.3 检查实现 diff 只包含本变更及必要双语文案/文档，保留无关编辑，核对无数据库/网络/Worker/更新协议改动；记录仍需的平台验证和规范归档依赖。完成标准为要求与对应验证均有结果，不能只以“已能构建”勾选。

## Workflow follow-up

- 已按用户的 apply 请求实施；用户另行确认保留 MyGo 原生双击打开详情，并完成 Windows Ctrl+单击人工验收。
- 归档顺序：`migrate-to-mygo-native-ui` → `restore-ui-parity` → `restore-ui-behavior-parity` → 本变更。不要为满足顺序提前勾选前序实机或签名发布任务。
- 前序 delta 合入主规范后，重新校验本变更的 MODIFIED/REMOVED 目标；归档后校验最终主规范，确认不会被后归档的旧点击语义覆盖。
