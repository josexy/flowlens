# Proposal

## Why

当前 MyGo 实现已覆盖原 FlowLens 1.0.5 的主要页面与业务入口，但前两轮对齐把“尽可能复刻旧前端”写成了默认要求，甚至覆盖了 MyGo 自带的表格选择行为。按本轮要求，需要把验收标准调整为“业务语义保持、MyGo 原生交互优先”，并处理代码审查确认的键盘入口不足和局部布局适配问题。

## What Changes

- 以两份当前工作树为依据建立逐模块差异记录，详见 `design.md`：区分已覆盖能力、合理原生差异、继承自旧前端的局限、待改善项和未完成的实机验证。原项目只读。
- 将视觉基准改为信息层级、可读性和操作可达性；不再要求像素、行高、动画、组件位置逐项复制旧前端。保留已完成且有价值的展示细节。
- **BREAKING（交互习惯）** 流量表恢复 MyGo 的普通单选、Ctrl/Cmd 增减选择、Shift 范围选择与原生拖选。选择与详情展开分离：Enter/无修饰键双击/“显示”打开详情，显式关闭按钮收起详情；普通选择在详情已打开时更新内容。
- 给已定位的自绘导航项和可编辑网格补齐键盘焦点与激活入口，复用 MyGo/nui 基础控件与现有业务函数，不新增窗口级键盘监听。
- 内存统计页按实际可用内容宽度换行、调整摘要和指标列数，覆盖主窗口最小尺寸及侧栏最大宽度组合；不复制 CSS 断点。
- 保留原生编辑器、虚拟 Hex、帮助 Slideover、原生菜单和图表交互。记录内存图表没有旧版拖框缩放/双击重置这一能力差异，本轮不新增缩放实现。
- 更新差异文档、使用说明、相关中英文文案与验证用例，使后续工作不再把合理原生差异误判为缺陷。

## Capabilities

### New Capabilities

无新增业务能力。

### Modified Capabilities

`openspec list --specs` 当前为空。以下沿用三个未归档变更已定义的 capability 路径，不另建同义能力；本变更是这些 delta 之后的后继变更，合并次序与冲突在 `design.md` 中明确列出。

- `appearance`：替换以 1.0.5 为强制视觉基准的要求。
- `traffic-capture`：采用 MyGo 表格选择模型，保留排序、过滤、淘汰与历史切换语义。
- `traffic-inspection`：详情采用显式打开/关闭操作，保留当前信息和数据加载生命周期。
- `interaction-conventions`：已审查的导航项支持原生键盘操作。
- `request-workspace`：网格单元格支持键盘进入和退出编辑，保持有序字段及现有提交语义。
- `memory-statistics`：内容变窄时布局仍保持操作可达和指标可读。

## Impact

- 实现范围：`desktop/capture`、`desktop/inspect`、`desktop/widgets`、`desktop/settings`、`desktop/collection`、`desktop/plugins`、`desktop/rewrite`、`desktop/memstats`，以及按需调整调用方 `desktop/editor`、相关测试、`locales/` 和文档。
- 保持现有 `desktop/store`、`desktop/logic`、`desktop/runtime` 边界；MyGo v0.8.3 / nuxt-ui-go v0.0.2 不升级。
- 不改变 SQLite/HBIN/HAR、请求模型、网络传输、Python Worker/SDK、进程归属与图标缓存、系统快捷键、CA/系统代理、窗口退出/更新协议；不新增偏好设置或旧/新交互双模式。
- 提案阶段只生成规划文件；后续实现与验证记录见 `verification.md`。代码审查与现有测试不能替代 Windows/macOS/Linux 的 IME、DPI、菜单、拖放和窗口实机验收。
