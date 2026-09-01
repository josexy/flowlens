# AGENTS.md

FlowLens 项目协作规范，面向开发者与代码代理。重点记录模块边界、高风险契约和验证要求；产品能力与使用说明以 README 和 `docs/` 为准。

## 1. 项目与技术栈

FlowLens 是基于 MITM 的跨平台桌面抓包工具，使用 `Wails v3 + Go 1.27+ + Vue 3 + TypeScript` 构建。核心能力包括 HTTP/HTTPS 与 SOCKS5 代理、WebSocket、请求编辑与重发、API Collection、历史与 HAR、进程归属、Python 插件、证书、快捷键及本地设置。

- 后端：Go、Wails v3、SQLite（`modernc.org/sqlite`）、`mitmproxy-go`、`xhttp`、`websocket`、uTLS、`logx`
- 前端：Vue 3、Vite、Vue Router、Pinia、Nuxt UI v4、Tailwind CSS v4、Monaco、Lucide
- 可选运行时：外部 CPython 3.11+；不嵌入 CPython、不依赖 CGO
- 工具链：npm、ESLint、Tailwind CSS language server、Go test、TypeScript utility tests

## 2. 模块边界

- `main.go`：只负责嵌入资源并启动 `backend/app`。
- `backend/app`：Wails 装配、SQLite 初始化、窗口/托盘/单实例生命周期、事件桥接与安全退出。
- `backend/services/proxy_service`：代理核心、请求编辑与合成传输、WebSocket、重发、计时/大小采集、HBIN/HAR 和前端实时事件。
- `backend/services/history_service`：历史读取、删除、重发和导出入口；共享编码与 HAR 实现仍由 `proxy_service` 提供。
- `backend/services/python_plugin_service`：插件注册、revision、规则、Worker、帧协议、SDK、请求 hook 和实时日志。
- `backend/services/api_collection_service`：API Collection SQLite 仓储、树操作、事务和托管请求体文件。
- `backend/pkg/process_attribution`：跨平台进程查询、异步 Manager、身份/图标缓存；`proxy_service` 只负责接入和生命周期。
- `backend/services/{setting_service,logging_service,shortcut_service}`：设置、日志和系统级快捷键的唯一业务入口。
- `backend/pkg/{body_cache,body_spool,database,logger}`：Body 缓存、Python Body 临时文件、共享数据库和日志基础设施。
- `frontend/src/stores`：跨组件状态；优先复用 `trafficWorkspace`、`apiCollection`、`workbench`、`setting` 等现有 store。
- `frontend/src/components/traffic-workspace`：抓包、历史、分类、请求编辑和 WebSocket 客户端界面。
- `frontend/src/shortcuts`：应用内命令目录、绑定解析、冲突检测和单窗口调度。
- `frontend/bindings`：Wails 生成物，通过 `#bindings/*` 使用，不手工维护。
- `docs/technical/python-plugins*.md` 与 `docs/examples/python-plugins`：Python 插件用户契约和示例。

修改应放在既有业务所有者内，不为单次需求增加平行实现或无必要抽象。

## 3. 通用协作规则

- 需求存在两种以上合理解释且会显著改变结果时，先说明差异；其余情况基于现有代码做合理假设并推进。
- 保留与任务无关的工作区改动。提交时只纳入当前任务范围，不混入顺手格式化或无关修复。
- Go 沿用现有 service/package 划分；Vue 沿用 `script setup`、Pinia、Nuxt UI 和 Tailwind v4 组织方式。
- 基础控件直接使用 Nuxt UI；只有明确的应用级组合行为才新增共享组件。
- 用户可见文案进入 `frontend/src/locales/{zh,en}.json`，不在组件中硬编码。
- 新增或调整 Wails 导出接口/模型后执行 `wails3 generate bindings -ts -i`，生成物随改动提交。
- 代码改动必须附带与风险匹配的最小验证；无法执行时明确说明原因和剩余风险。

## 4. 高风险契约

### 4.1 网络、Header、历史与 HAR

- 代理与请求编辑统一使用带内置 HTTP/2 支持的 `github.com/josexy/xhttp`，不要混用标准库 HTTP 类型或其他 HTTP/2 实现。
- Header/Trailer 对外模型保持 `[]HTTPHeaderField`。原始 HeaderBlock 可用时保留行顺序、重复项、大小写、空值和截断状态；不可用时才降级为规范化字段并标记线序不可用，不得退回 map。
- HTTP 请求规范化集中在 `synthetic_request_headers.go`：URL 决定路由；伪 Header、`Host`、framing、生成的内容类型和 fallback UA 由后端维护。目标传输无法无损表达 HeaderOrder 时必须报错。
- 合成传输与共享 TLS dialer 集中在 `synthetic_transport.go`。显式 HTTP/1.1 不应用 HTTP/2 指纹；重定向保持指纹语义；协议、代理和指纹配置通过 API Collection 完整往返。
- `HTTPMessageMetrics` 只能来自传输边界事件，并保持微秒精度、重试隔离和 capture generation 隔离。失败、取消或不完整 Body 不得伪造完成值，未知数值使用 `-1`。
- `HeaderSize` 是字段行逻辑大小，不含 start line、TCP/TLS、HPACK 或 frame 开销；`BodySize` 是传输层编码后的实体 Body 字节数。后端时间戳统一保存 Unix 微秒。
- 当前历史格式是 HBIN v1，不兼容更早开发态布局。未知版本应跳过且不得删除；模型变化需同步 codec、历史测试、bindings 和前端。
- HAR 生成与流式原子写入统一复用 `proxy_service` 的 `HARFileWriter`。区分空 Body 与缓存 Body 缺失，保留其余可导出项并统计 skipped/missingBodies。HAR 不会自动脱敏凭据、Cookie、Body 或进程路径。
- API Collection 写操作必须保持 SQLite 事务与托管请求体文件一致，覆盖失败回滚、孤儿清理和启动校验。

### 4.2 Python 插件

- 插件只进入 HTTP 请求编辑器，不进入普通 MITM 抓包、重发或 WebSocket 客户端。
- 外部 CPython Worker 使用版本化长度前缀 JSON 协议，并对帧、源码、Body、参数、共享状态、输出和 hook 时间设界。超时、取消、崩溃或协议损坏时终止并替换 Worker；Windows 同时清理子进程树。
- `-I` 只隔离导入环境，不是安全沙箱；插件始终按可信代码处理。
- 每次发送前快照匹配的插件顺序、revision 和参数。请求链为“全局插件 -> 当前请求脚本 -> 网络”，响应链反向执行；请求阶段 fail-closed，响应阶段 fail-open。
- 不同插件的 `context.shared` 相互隔离；同一插件的请求/响应 hook 可共享 JSON 状态。
- Body 的用户语义与内部存储表示分离。超过 4 MiB 的编辑请求和普通响应使用 `body_spool`；文件字节不得嵌入 Worker JSON 帧，也不得暴露或持久化临时路径。
- FlowLens 托管的临时文件必须在成功、阻断、失败、超时、取消和 Worker 退出后清理。SSE 只允许在响应开始前修改状态和 Header，不接受 Body/Trailer 修改，也不创建 spool 文件。
- 全局插件信息、规则、参数和有效 revision 由 SQLite 与托管目录持久化。当前请求脚本源码可随 API Collection HTTP 请求保存；全局插件旁路开关和当前脚本启用状态只属于当前标签页，重新打开时默认关闭。
- 完整运行输出只进入携带 execution ID 的请求编辑控制台；不要恢复独立的全局日志环或插件工作台日志历史。

### 4.3 进程归属与图标

- 进程查询必须异步，不能阻塞代理连接；仅本机直连连接参与，远程客户端明确跳过。
- 进程身份使用 PID + `StartToken`，队列、TTL、容量和查询超时保持有界。迟到结果必须验证连接和记录仍有效。
- 清理图标缓存时协调 Manager 轮换/关闭、后台任务取消、旧 `IconKey` 失效、磁盘文件删除和前端窗口缓存失效；磁盘文件丢失后应能按需恢复。
- 前端统一使用 `AppProcessIcon.vue` 与 `processIconCache.ts`，不要在业务组件中直接调用 `GetProcessIcon` 或另建缓存。
- `ProcessInfo` 变化时同步代理模型、HBIN codec/测试、历史、bindings 和前端展示。

### 4.4 设置、日志、事件与快捷键

- 设置通过 `setting_service` 维护默认值、校验、分区序列化和 SQLite 持久化；日志统一通过 `backend/pkg/logger` 与 `logging_service`。
- 前端 `Events.On()` 必须保存返回的 off 函数，并在组件卸载或 store `cleanup()` 时调用；不要用 `Events.Off(eventName)` 清理同名监听。
- 系统级快捷键只能由 `shortcut_service` 注册，保持白名单、先注册后持久化、失败回滚和 shutdown 清理。
- 应用内快捷键通过 `frontend/src/shortcuts` 与 `AppShortcutHost`；按钮、菜单和快捷键复用同一业务函数，不新增组件级平行 `keydown`。
- handler 的 `when` 必须检查真实活动页面和标签状态。输入控件与 Monaco 默认保留自身按键，只有 `editablePolicy: 'allow'` 的命令可在编辑状态执行。
- 快捷键展示统一读取 resolved binding。`primary` 在 macOS 为 Command，在 Windows/Linux 为 Ctrl，不手工拼接平台文案。
- override 缺失表示使用默认绑定，`binding: null` 表示显式禁用。普通设置保存使用 `UpdatePreservingShortcuts`；快捷键变更只通过 `ApplyShortcutConfig`。

### 4.5 前端与文案

- UI 优先使用 Nuxt UI 原生组件、Tailwind v4 canonical class、语义色类和 `UIcon`/`i-lucide-*`；不要引入新的基础控件封装或 `@vicons/*`。
- `USelect` / `SelectItem` 的值不能是空字符串；需要空值时在组件内使用哨兵映射。
- Header/Trailer/Cookie 编辑与复制复用 `frontend/src/utils/{headers,cookies}.ts`，保留顺序、重复项、大小写、空值和降级提示。
- 日期、微秒时间、耗时和大小复用 `frontend/src/utils/format.ts`，不要在组件中自行格式化后端原始值。
- HAR 菜单和提示复用 `useHARExport.ts`，保留来源、ID 顺序、扩展名补全、并发保护和结果统计。
- 新文案必须补齐中英文并保持叶子 key、占位符和状态条件一致。不要因当前译文相同而合并生命周期不同的 key。
- 常驻区域使用短文案，避免标题、描述、状态和按钮重复；删除、覆盖、取消、重启生效、截断/线序降级等高风险语义不得省略。
- 影响主题的交互至少检查 light/dark；影响托盘、标题栏或窗口显隐时同步检查 `backend/app`、`App.vue`、`setting` store 和 locales。

## 5. 开发前影响检查

实现前按改动范围确认：

- 是否改变 SQLite schema、事务、设置持久化、API Collection 托管文件或缓存清理。
- 是否改变 HTTP 模型/指标、Header/Trailer、Body 可用性、证书时间、HBIN/HAR 或协议/指纹。
- 是否改变 Python Worker、hook 顺序、临时文件、SSE 限制、execution ID 或脚本持久化边界。
- 是否改变进程归属、连接关闭竞态、跨平台 provider、图标缓存或 `ProcessInfo`。
- 是否改变 Wails 事件、快捷键命令、窗口/托盘生命周期、bindings、i18n、文档或测试。

多步骤任务先明确“修改内容、验证方式、完成标准”。

## 6. 开发与验证命令

环境要求：Go、Node.js 20.19+ 或 22.12+、npm、wails3；Task 和 Python 3.11+ 按功能选用。

```shell
wails3 generate bindings -ts -i
task dev
task build
task package
task run
task version VERSION=1.2.3
task version VERSION=1.2.3 CHECK=true
```

直接启动开发模式使用 `wails3 dev -config ./build/config.yml`。桌面前端端口默认 `9245`，可由 `WAILS_VITE_PORT` 覆盖；前端 UI 必须通过 Wails 桌面窗口调试。

后端默认验证：

```shell
go test ./...
```

按范围追加：

- SQLite：`go test ./backend/pkg/database ./backend/services/setting_service ./backend/services/api_collection_service`
- 快捷键：`go test ./backend/services/setting_service ./backend/services/shortcut_service`
- 进程归属：`go test ./backend/pkg/process_attribution ./backend/services/proxy_service ./backend/services/history_service ./backend/services/setting_service`
- 指标、Body、HBIN、HAR：`go test ./backend/services/proxy_service ./backend/services/history_service`
- Python 插件：`go test ./backend/services/python_plugin_service ./backend/services/proxy_service ./backend/services/setting_service`
- 并发/生命周期：工具链支持时执行 `go test -race ./backend/pkg/process_attribution ./backend/services/proxy_service`

涉及 Worker 协议、SDK、解释器或第三方包时，确认 Python 3.11+ 集成测试确实执行而非被跳过。

前端验证：

```shell
cd frontend
npm run type-check
npm run lint
npm run test:process-icon-cache
npm run test:request-editor-state
npm run test:traffic-utils
npm run lint:tailwind
npm run build
```

按范围选择最小集合；组件结构、主题或生产打包相关改动执行 `npm run build`。Tailwind 全量诊断使用 `npm run lint:tailwind -- --all`，自动修复只使用显式的 `lint:fix` 或 `lint:tailwind -- --fix`。

## 7. 文档、提交与完成标准

- Python 插件入口、作用域、hook 契约、执行顺序、限制、安全模型或控制台变化时，同步中英文技术文档、示例和 README。
- 提交使用英文 Conventional Commits：`<type>(<scope>): <summary>`。一次提交只解决一类问题；较大改动补充简短 description。
- 提交前确认：范围内文件已验证、无无关改动、生成物已同步、双语文案一致、前后端接口匹配。
- 完成时说明实现结果、验证命令和任何未解决风险；不要把“理论可行”当作完成。
