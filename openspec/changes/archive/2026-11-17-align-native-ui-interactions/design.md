# Design

## Context

动机见 `proposal.md`。本文件同时保存本次代码审查结果和后续实现决策；“当前事实”与“拟议目标”分别标明。

### 审查基线与可信范围

审查日期：2026-11-13。两边开始检查时 `git status --short` 均为空。

| 标识 | 当前源码 | 提交 |
| --- | --- | --- |
| M | `E:/Codes/Github/flowlens-mygo` | `78e6c2e2e04266b24b8f4e622a6a8911b5f49c6c` |
| O | `E:/Codes/Golang/src/flowlens` | `972cf419b60551bea63813b8c4dfe42b3a78c8c2` |

O 的 `frontend/package.json` 为 1.0.5，技术栈是 Wails + Vue/Pinia/Nuxt UI/Monaco；这里把它称为“原前端”，以免与 MyGo 原生界面混淆。M 使用 MyGo v0.8.3、nuxt-ui-go v0.0.2、Go 1.27.3。

以下证据路径以 M 或 O 为根；O 的 `components/`、`views/`、`stores/` 均位于 `frontend/src/`，表中的 `traffic/`、`common/`、`memstats/` 等组件分组简写位于 `frontend/src/components/`。M 的 `capture/`、`shell/`、`settings/` 等 UI 模块简写均位于 `desktop/`。审查覆盖各主要 surface 的组件结构、关键事件入口和状态持有方式，重点追踪表格选择、详情、编辑、导航、侧栏、图表与关闭流程；并非逐行证明两仓库所有功能等价。没有启动两套真实窗口做截图比对，不能据此断言 DPI、IME 或平台窗口行为完全相同。

### 逐模块对照

“已覆盖”表示本次读取的入口及流程存在，不等于所有分支和平台都已验收。

| 区域 | 原前端证据 | MyGo 证据 | 当前判断与处理 |
| --- | --- | --- | --- |
| 主框架与页面 | O `views/MainView.vue`、`components/workbench/{ContentHost,SecondarySidebarHost}.vue`、`stores/workbench.ts` | M `desktop/shell/{app,sidebar}.go`、`desktop/store/workbench.go`、`backend/app/app.go` | 六个主栏目、可折叠次侧栏、内容区、状态栏已覆盖。20% 默认侧栏、15–42% 拖动范围相同；Vue 保留已挂载视图，Go 持有 store/view state 并只构建当前 surface。属于合理架构替换。 |
| 侧栏折叠 | O `MainView.vue` 的 `collapsible` 面板及 collapse/expand 事件 | M `shell/app.go: workspaceMain`，`WorkbenchStore.ActivateSection` | M 支持重复点击主栏目收起/展开，但分割器自身不实现 O 的拖到 0 折叠。可接受的入口差异，保留显式切换。 |
| 捕获工具栏、过滤、历史 | O `traffic/{ProxyToolbar,FilterBar}.vue`、`traffic-workspace/WorkspaceSidebar.vue` | M `capture/{toolbar,filterbar,history,workspace,export}.go` | 启停、快速代理配置、DNS、系统代理、清空、会话、HAR/CSV 入口存在；工具栏仍位于工作区标签上方。没有发现需要重新搬移工具栏的依据。 |
| 流量表 | O `traffic/TrafficTable.vue:608` 起 | M `capture/selection.go:9` 起、`table.go: Build` | 目前主动复刻原点击语义，并覆盖原生选择。属于已确认的原生优先冲突，见 F1。 |
| 详情容器 | O `traffic/DetailPanel.vue` | M `inspect/panel.go`、`capture/workspace.go` | 请求/响应分栏、适用标签、错误页与状态摘要已覆盖；无独立关闭按钮是刻意复刻，不是 MyGo 限制。与 F1 一并调整。 |
| Headers/Query/Cookies/TLS | O `traffic/{HeadersTable,QueryTablePane,ResponseCookieTable}.vue`、`traffic/detail/DetailTlsSection.vue` | M `inspect/{tables,overview}.go`、`widgets/field_grid.go`、`logic/{headers,cookies}.go` | 有序字段、紧凑表格、完整值查看、Cookie 属性与 TLS 信息已有对应实现。保持信息内容和复制语义，不要求相同 DOM 结构、列像素或浮层几何。 |
| Body/Raw/Hex/Image | O `traffic/BodyViewer.vue`、`common/{HexDumpViewer,MonacoBodyEditor}.vue` | M `inspect/{body,raw,hex,image}.go`、`widgets/code_view.go` | 原生编辑器和虚拟列表替换 WebView；Hex 不需要原分段分页/大数据门槛。原生语法色彩、查找栏、字体与滚动条不同可以接受。 |
| SSE/WS 明细 | O `traffic/SseMessageViewer.vue`、`common/websocket/WebSocketMessageStream.vue`、`modal/WebSocketMessageDetailModal.vue` | M `inspect/{sse,messages}.go`、`widgets/{detail_dialog,row_activator}.go` | 列表、会话、详情、复制/保存/换行已覆盖。表格作为单一焦点容器、方向键移动、Enter 激活是合理原生差异；文字气泡可点击是已有扩展。 |
| HTTP/WS 编辑器 | O `traffic-workspace/{HttpRequestEditorPane,WebSocketClientPane,RequestUrlInput}.vue` | M `editor/{http,websocket,requestline,settings,response}.go`、`store/workspace.go` | URL/方法、请求页签、发送/停止、协议/代理/TLS、响应页与脚本入口均有实现。使用同一个请求 store 管理异步结果；未因原生优先而扩大网络语义改动。 |
| 工作区标签 | O `traffic-workspace/WorkspaceTabBar.vue` | M `editor/tabbar.go`、`store/workspace.go` | 固定 Capture、关闭变体、脏标记、拖动、激活可见与快捷键已覆盖。保留这些工作流；动画曲线和标签精确宽度不作为等价条件。 |
| 键值编辑网格 | O `traffic-workspace/EditableKeyValueTable.vue:235,296`、`common/CellTextareaEditor.vue` | M `widgets/editable_grid.go:197` 起、`editor/rows.go` | 两边都以点击进入编辑为主要入口。M 单元格只有 Row/OnClick，没有可聚焦基础控件；这是继承旧设计的可达性局限，非新增迁移回归，见 F3。 |
| Python 控制台 | O `traffic-workspace/HttpRequestPythonConsole.vue` | M `editor/console.go`、`logic/python_console.go` | 列、执行状态、详情、清空、复制和保存均有实现；只读代码视图带行号不同于原 textarea，可保留。 |
| API Collection | O `api-collection/ApiCollectionTree.vue`、`stores/apiCollection.ts` | M `collection/{view,save}.go`、`store/collection.go` | 目录、请求打开/保存、重排、多选删除入口存在；Move to Root 为原生版本扩展。自绘树行缺少键盘激活入口，见 F2。 |
| Rewrite | O `rewrite-rules/{RewriteRulesSidebar,RewriteRulesSurface,RewriteRuleForm,RewriteDraftGuard}.vue` | M `rewrite/{view,form}.go`、`store/rewrite.go` | 规则开关、草稿、冲突、预览、保存/放弃/取消和退出守卫有对应实现；自绘规则列表选择入口归入 F2。 |
| Python 插件 | O `python-plugins/{PythonPluginSurface,PythonPluginWorkspaceSidebar}.vue`、`stores/pythonPlugins.ts` | M `plugins/{view,rules}.go`、`store/plugins.go` | 创建、文件树、编辑、参数、规则、校验/重载、草稿切换均有对应实现；导航/文件行键盘入口归入 F2。Worker 与 SDK 不在本变更范围。 |
| Python 帮助 | O `workbench/ContentHost.vue:27` 起 | M `plugins/help.go:90` 起、`shell/app.go` | O 内容区 ≥1100 时停靠，否则抽屉；M 始终为 420 宽模态 Slideover。位置与是否阻塞背景不同，既有明确取舍，保留；不要求恢复停靠。 |
| 设置 | O `views/SettingsView.vue`、`components/settings/` | M `settings/{view,general,proxy,pages}.go`、`store/settings.go` | 分组、草稿保存、重置、退出确认、证书/解释器/快捷键入口已覆盖。原导航有 tabindex 与 Enter/Space handler，M 为 `Row.Role(Button).OnClick`，缺少实际键盘焦点，见 F2。Pointer Cursor 已移除，为既有明确差异。 |
| 内存统计 | O `memstats/{MemStatsPanel,MemStatsChart}.vue` | M `memstats/view.go`、`store/memstats.go` | 指标和采样控制已有实现；固定列数缺少内容宽度适配（F4）。图表换成原生组件后增加键盘采样定位/图例切换，但没有旧版拖框缩放（D1）。 |
| 更新/重置/窗口 | O `views/{UpdateView,ResetStartupView}.vue`、设置退出事件 | M `update/view.go`、`resetstartup/view.go`、`backend/app/{lifecycle,settings_window,update_restart}.go` | UI 阶段、取消/后台运行、失败重试、设置与 Rewrite 退出守卫存在；跨 WebView 事件改为共享 store/dispatcher。真实更新签名链路、系统窗口、托盘/菜单仍沿用迁移变更的实机验收任务，未在此声称完成。 |

### 已确认的待改善项

**F1 — 流量选择覆盖原生行为（高优先级，需求冲突）。**

M `capture/selection.go` 明确写出“rewrites what a click chose”；`beginSelection` 把 Selected 设为 -1 探测，`resolveSelection` 恢复快照，`applyClick` 再实现 Shift 单项切换、Ctrl/Cmd 详情开关、单击唯一已选项清空。M `table.go: Build` 把这些步骤包围在 `ui.Table` 前后。这不是能力缺失，也不是本次发现的崩溃：当前行为与 O 一致，但不符合用户本轮原生优先方向，并增加丢失 pointer-up、快速拖动、失焦的维护复杂度。

MyGo 固定版本的 `ui/list.go` 已提供多选、范围选择、拖选、键盘移动和 Enter 的 Submitted 通知。应使用该行为，并为详情补显式入口；不能只删除覆盖层而让详情无法打开。

**F2 — 自绘导航有点击语义，没有相应的焦点语义（高优先级）。**

确认调用点：M `settings/view.go:453`、`collection/view.go: row`、`plugins/view.go: row/fileRow`、`rewrite/view.go: row`、`capture/workspace.go: captureItem`、`capture/history.go: historyRow`、`capture/category.go: categoryRow/categorySectionRow`、`memstats/view.go: Sidebar`。这些 Row/Column 的 OnClick 不等同于 ButtonBase。MyGo `ui/actions.go:25` 只设置 clickable，`ui/access.go:167` 的 Role 只改辅助技术角色；均不创建 Tab 焦点。Category 虽使用 List 容器，也没有把行选择/提交接到 `activateCategoryRow`。

O 设置导航使用 `div role="button" tabindex="0"` 并处理 Enter/Space，因此此处是可定位的键盘回归；其他树/列表中也有旧版仅 div 点击写法，不能全部称为迁移回归。现有“对话框自动获得焦点”测试与此不同，不能证明导航可键盘操作。

**F3 — 请求网格缺少键盘进入编辑（中优先级，继承限制）。**

M `editableGridTextCell` 只设置 Label/OnClick；`editableGridEditor` 在已打开后才创建 Textarea 并自动聚焦。现有测试覆盖点击、Enter、blur、composition，但没有从 Tab/Shift+Tab 到单元格并激活的流程。保留紧凑网格和可增长编辑器，同时补可聚焦激活面与退出后的焦点回落，不必为了原生化推倒已有编辑模式。

**F4 — 内存页列数不随可用空间调整（中优先级，布局差异/可用性风险）。**

O `MemStatsPanel.vue:157,209,244` 使用工具栏 flex-wrap、2/4 摘要列、1/2 指标列。M `memstats/view.go:101,134,230` 固定一排动作、四张摘要、两列指标。M 主窗最小 1024×768，主侧栏 52，次侧栏可占剩余宽度 42%，内容区约为 560（扣分隔器后略少）；这个组合应纳入验收。代码已确认缺少适配，但本轮没有渲染这个组合，不把具体裁剪位置或溢出像素当作已观察结果。

**D1 — 内存图表缩放差异（能力差异，本轮接受）。**

O `MemStatsChart.vue:232` 起启用 x 轴拖框缩放与 dblclick reset。M 只使用 `nui.Chart`；固定依赖 `chart.go` 提供采样提示、方向键和图例显隐，没有相同的缩放设置。保留现有原生图表，不实现额外缩放层；文档明确这项差异，避免继续写“完全一致”。

**D2 — 规划与文档的默认规则冲突（高优先级）。**

`docs/technical/ui-parity.md` 首段及 `restore-ui-parity/specs/appearance/spec.md` 要求位置、尺寸、颜色尽量匹配；`restore-ui-behavior-parity` 要求旧点击模型并禁止详情关闭按钮。本变更明确改变这些要求，不把它们隐藏成“实现细节修复”。前两次工作恢复的完整内容、错误提示、保存格式和禁用状态仍有价值。

## Goals / Non-Goals

**Goals:** 使用固定版本原生控件的状态和输入语义；保留业务对象身份、异步防陈旧结果、数据格式与操作反馈；让键盘与鼠标到达同一业务函数；为每项接受的差异留下依据。

**Non-Goals:** 全面重新设计所有页面；恢复 Monaco、DOM 分页、uPlot 或 CSS 动画；增加一个“经典前端行为”设置；调整 Worker、安全/持久化/网络契约；仅为截图一致而移动现有控件；重做先前已经实现的功能。

## Decisions

### 1. 用信息与任务结果做视觉验收

保留主框架、业务分区和已有 theme/nui 风格。视觉要求改为：关键字段可读、重要动作可发现、状态区分清楚、浅深主题可用。尺寸、间距、分隔线、滚动条、菜单和帮助面板位置允许原生差异。移除 appearance 中强制对齐 1.0.5 的 requirement，以同一 capability 内的 Native interface usability 替代；其他修改使用原 requirement 名称与完整更新内容。拒绝继续扩充逐像素例外名单，也不因此批量删除已有样式。

### 2. 表格选择与详情状态分开

- `ui.ListState`/`ui.Selection[uint64]` 继续由 Table 持有，ID 仍为身份；消除左键选择的快照恢复和重写流程。保留右键定位所需的最小观察逻辑，不误删 header/row 上下文菜单分流。
- 单击单选且重复单击保持选择；Ctrl/Cmd 增减、Shift 连续范围；拖选和组合修饰键遵从固定 MyGo 版本。方向键、Select All 与平台特有按键保持原生语义。
- 详情关闭时，选择不打开它；Enter/无修饰键双击（表格 Submitted）与现有“显示”业务函数打开当前有效焦点行。Enter 可以在多选时打开焦点行；菜单“显示”仍只在一行选择时可用。
- 不新增应用级 Enter 快捷键。使用控件提交事件，避免截获文本输入、模态框和其他列表的 Enter。
- `inspect.Panel` 通过回调呈现显式关闭按钮，由 Capture Workspace 写入对应 Source 的 ShowDetail；关闭保留选择并回落到触发的表格焦点。再次选择不会自动重开。
- 详情打开后跟随有效选择焦点；若焦点行被取消选择，选择仍存时转到表格顺序中的首个已选行；选空则清理详情。切换历史、删除/过滤/淘汰、异步 body 结果继续走现有 source/store。
- 使用原生 Submitted 事件包含的无修饰键双击打开详情，不另建双击检测；有 Ctrl/Cmd/Shift 的提交通知只改变选择。2026-11-13 实施时核实固定版本的 Submitted 同时包含 Enter/双击，用户明确确认保留原生双击行为。

备选方案是保留旧 Shift/Ctrl 习惯只改文档；不采用，因为用户明确要求原生行为优先。另一方案为点击立即展开；不采用，以保留“先批量选行再操作”的空间稳定性。

### 3. 导航和网格复用基础控件，不接管窗口键盘

对 F2 的具体调用点使用 ButtonBase 或原生 List/Tree 的选择/提交能力。简单设置/单条侧栏用 ButtonBase；含开关、checkbox、菜单或拖动柄的行，给名称/主操作区独立焦点，避免嵌套控件激活整行。所有入口调用现有 select/open/toggle 函数，不能复制存储事务或发送逻辑。保留稳定的对象 key、可见焦点、禁用状态和已有拖动反馈。

网格名称/值使用可聚焦激活面；Enter/Space/点击进入已有 Textarea。保留 Enter（含 Shift）和 blur 的现有提交规则，IME composition 的 Enter 不触发提交。Escape 取消该次编辑，恢复打开该编辑器时的字段值，符合既有 interaction-conventions 的取消语义；Tab/Shift+Tab 提交后移到下一/上一可操作控件。Enter/Escape 结束后焦点回到原单元格，行被删除则落到仍存在的邻近行或容器。使用 Handle、原值快照与稳定身份，不保存 Element 或 Context，不对已经消失的行执行恢复。

选择这些有限适配，避免把所有树和网格一次性重写成新抽象。键盘可达不要求每行都成为独立 Tab stop；原生列表可采用一个 Tab stop 加方向键，但必须能到达并激活每个可见项目。

### 4. 内存页按内容宽度适配

读取自身内容区可用宽度，不能仅用窗口宽度或主题 CSS 断点。摘要卡设置可读最小宽度并折行，指标区在两列无法保持值区域时转单列；工具栏允许动作换行并保持全部动作可见。图表继续填满容器。布局分支不得重建或重置 MemStatsStore/ChartState，不改变采样、暂停、清空或数据保留。

具体宽度阈值属于实现可调参数，以 1024×768 主窗、15/20/42% 次侧栏、1600×860、两种主题与语言、最大配置字体的可达性结果确定。这里不先承诺原 CSS 断点或额外全局响应式框架。

### 5. 保持现有高风险边界

| 边界 | 本变更处理 |
| --- | --- |
| 持久化/文件 | 不新增 schema，不改变请求 body 文件、HBIN/HAR 或导出命名；选择导出仍经 capture/export。 |
| 网络/模型 | 不改请求归一化、HeaderField 顺序、transport 或 metrics。 |
| Python | UI 激活与焦点可变；Worker/SDK/顺序/输出作用域不变。修改插件 UI 后实际运行 Python 测试。 |
| 进程/图标 | 继续复用 widgets.ProcessIcon，无新查询或缓存。 |
| 事件/快捷键 | 控件内键盘交给基础控件；应用命令仍通过 desktop/shortcuts，不新增 global handler。 |
| 窗口/退出 | 不改主窗最小尺寸、托盘、CA、系统代理、更新或退出守卫；关闭详情不关闭窗口。 |
| 文案/文档 | 新关闭详情文案双语；更新点击/修饰键说明和差异记录，保留删除、覆盖、取消等语义。 |

## Risks / Trade-offs

- [改变已经发布的 Shift/Ctrl 习惯] → 在使用说明和版本说明明确；不提供两套输入模式。
- [右键定位与左键兼容层共用代码] → 删除前分离右键的最小路径，验证 header menu、行菜单和多选目标。
- [增加焦点入口影响嵌套开关/拖动] → 主激活区与行内动作分离；分别验证鼠标、Tab、Enter、Space 和拖动。
- [移除自绘选择后视觉状态与 source 不一致] → 针对选择数、焦点 ID、详情、排序/过滤/淘汰、历史切换做状态联合断言。
- [原生化被误解为重新移植全部组件] → 只实施 F1–F4 与文档规则调整；D1 与其他已接受差异不进入功能恢复任务。
- [无显示测试不能证明平台可用性] → 区分已跑测试与待实机验收，不把旧变更未完成任务标成完成。

## Migration Plan

1. 以当前代码作为实现基线；先落实新的要求，再按 tasks 的独立小组修改，不回退前两轮已完成代码。
2. `openspec/specs` 当前没有主规范。依赖序列为 `migrate-to-mygo-native-ui` → `restore-ui-parity` → `restore-ui-behavior-parity` → 本变更。涉及 MODIFIED 的完整基准是前序 delta。本变更可以在当前代码上实施，但不能先于前序 delta 归档；合并前序规范后再严格验证最终主规范。
3. 前序的未完成实机/发布签名任务保持原状态，本轮不修改或归档它们。不得因为为本变更生成规范而提前宣布前序完成。
4. 不迁移用户数据。需要回滚时回退本次 UI 与文档改动即可，原数据库和保存的表格布局继续可用。

### 提案阶段已执行的验证（历史记录）

以下记录属于 propose 阶段。后续 apply 的代码、自动化与 Windows 实机验收见 [verification.md](verification.md)。

- `CGO_ENABLED=0 go test ./desktop/...`：除 `desktop/plugins` 外所有有测试的包通过；插件 fixture 的 `exec.LookPath("python3")` 命中 WindowsApps 执行别名，Worker 启动 EOF / exit status 9009。
- 解释器核实：实际 CPython 位于 `D:/Python/Python312/python.exe`，版本 3.12.10。只在临时目录放置其 launcher 副本命名为 python3.exe，并为测试进程前置 PATH；未修改系统 PATH、应用配置或 Python 安装。
- `CGO_ENABLED=0 go test ./desktop/plugins -count=1 -v`：使用上述临时解释器入口，17 个测试全部通过，无跳过。首轮失败属于测试环境路径问题，不作为产品回归。
- `task vet:ui`：通过。
- `openspec validate align-native-ui-interactions --strict`：通过，无 warning；CLI 提示主规范尚不存在，因此 MODIFIED delta 不能提前归档，处理次序见 Migration Plan。
- 本轮未改产品代码，未跑全后端 `task verify`、race 或原前端构建。没有执行 GUI/DPI/IME/系统集成验证；这些仍需后续 apply 和实机阶段完成。
