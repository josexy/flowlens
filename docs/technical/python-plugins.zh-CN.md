# HTTP 请求编辑器 Python 插件

[English](python-plugins.md) | **简体中文**

本文面向希望使用 Python 自定义 HTTP 请求与响应的 FlowLens 用户，涵盖 Python 3 环境配置、两种脚本作用域、`onRequest`/`onResponse` Hook、Body API、大小与安全限制以及常见故障排查。

FlowLens 仅在 **HTTP 请求编辑器** 内提供两种 Python 脚本作用域：

1. **全局 Python 插件：** 在 **Python 插件** 工作台中管理，通过匹配规则自动触发。
2. **当前请求脚本：** 绑定到单个 HTTP 请求编辑器标签页。

两种作用域都可在请求发送前检查或修改请求，也可检查或修改返回的响应。Python Hook 绝不会作用于普通 MITM 抓包、重发或 WebSocket 客户端。当前请求脚本源码可随 **API Collections** 中的 HTTP 请求保存，但不会写入设置、HAR、历史记录或 HBIN 数据。

Python 插件为可选功能，默认关闭。

## 目录

- [选择脚本作用域](#选择脚本作用域)
- [应用内 API 帮助与系统提示词](#应用内-api-帮助与系统提示词)
- [安全模型](#安全模型)
- [配置 Python](#配置-python)
- [五分钟快速上手](#五分钟快速上手)
- [创建并运行全局插件](#创建并运行全局插件)
- [配置插件参数](#配置插件参数)
- [使用当前请求脚本](#使用当前请求脚本)
- [在控制台查看脚本输出](#在控制台查看脚本输出)
- [插件包结构与 Manifest](#插件包结构与-manifest)
- [匹配规则与执行顺序](#匹配规则与执行顺序)
- [Hook 契约](#hook-契约)
- [Context API](#context-api)
- [Request 与 Response API](#request-与-response-api)
- [Body API](#body-api)
- [已测试示例](#已测试示例)
- [流式响应、大小与失败限制](#流式响应大小与失败限制)
- [日志与诊断](#日志与诊断)
- [故障排查](#故障排查)

## 选择脚本作用域

| 作用域 | 适用场景 | 配置能力 | 持久化 |
| --- | --- | --- | --- |
| **全局 Python 插件** | 在多个请求或接口间复用处理逻辑 | 托管文件、匹配规则、插件级 JSON 参数、执行顺序与启用开关 | 由 FlowLens 持久化保存，跨会话可用 |
| **当前请求脚本** | 调试或定制单个 HTTP 请求 | 单个脚本与标签页内启用开关；无匹配规则和参数配置 | 源码可随 API Collection HTTP 请求保存；启用开关不保存，重新打开时默认关闭 |

两种作用域共用同一套同步 `onRequest` / `onResponse` API。初次体验可先使用当前请求脚本；需要复用或按方法与 URL 自动触发时，再迁移到全局插件。

## 应用内 API 帮助与系统提示词

点击 **Python 插件** 侧栏标题或 HTTP 请求编辑器 **Script** 工具栏中的书本图标，即可打开 **Python API 帮助**（无需先配置 Python 或创建插件）。帮助以 420 px 宽面板从窗口右侧滑出，按 `Esc`、点击面板外侧、离开工作台或切换请求标签页都会关闭它，且不会改动脚本内容。

- **快速上手：** 展示三步配置流程与核心 Hook 行为，支持按需展开最小脚本与运行规则。
- **API 参考：** 按类型分组且支持搜索（搜索时自动展开匹配分组）；签名包含参数与返回类型，使用说明与导入指引可单独展开。
- **复制系统提示词：** 按当前界面语言复制完整的 API v1 契约、运行约束、包含两个 Hook 的最小脚本模板以及需求占位。入口位置决定提示词面向全局插件（`main.py`、参数与匹配规则）还是当前请求脚本。搜索条件和当前帮助分组不会截断复制内容。将其粘贴至 AI 工具并替换需求占位即可生成脚本，启用前请务必审查代码。复制内容仅包含文档与模板，绝不包含你的插件源码、请求数据或参数值。

## 安全模型

请只运行你信任的代码。插件以普通本地 Python 代码运行，拥有与 FlowLens 相同的操作系统用户权限：可以读写本地文件、访问网络、启动子进程、导入已安装软件包，也可能通过日志泄露数据。Worker 使用 Python 隔离模式（`-I`）仅用于保证导入行为可预测，**并非**安全沙箱。

启用插件前，请审查插件源码及所有第三方依赖。FlowLens 不会自动安装 Python 依赖包。

## 配置 Python

安装 CPython 3，或创建一个专用虚拟环境以将插件依赖与系统其他 Python 应用隔离。

**Windows（PowerShell）：**

```powershell
py -3 -m venv C:\venvs\flowlens-plugins
C:\venvs\flowlens-plugins\Scripts\python.exe -m pip install requests
```

**macOS / Linux：**

```shell
python3 -m venv "$HOME/.venvs/flowlens-plugins"
"$HOME/.venvs/flowlens-plugins/bin/python" -m pip install requests
```

随后在 FlowLens 中完成配置：

1. 打开 **设置 > Python**。
2. 点击 **检测**，从平台注册信息、`PATH`、当前环境和常见安装位置中选择解释器；也可手动指定绝对可执行文件路径（虚拟环境在 Windows 上选择 `python.exe`，在 macOS/Linux 上选择 `bin/python`）。
3. 点击 **测试**，确认 FlowLens 报告该解释器可用。
4. 设置单次 Hook 超时时间（默认 `5,000 ms`，允许范围 `100–60,000 ms`）。
5. 开启 Python 插件开关并确认可信代码安全提示。

### 解释器检测说明

- 检测受时间与范围限制，不会扫描全盘；位于非常规位置的虚拟环境请手动选择。
- 在 Windows 上，检测还会识别当前用户注册的 Microsoft Store Python 包以及 Python Install Manager 列出的已安装运行时。Store Python 提供包专属执行别名（例如 `%LOCALAPPDATA%\Microsoft\WindowsApps\PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0\python.exe`），FlowLens 支持直接用该别名进行检测、测试与执行。建议优先使用检测到的别名，避免使用 `C:\Program Files\WindowsApps` 下带版本号的路径（商店更新后可能失效），也无需修改 `WindowsApps` 目录权限。
- 检测结果仅列出实际探测成功的 CPython 3 运行时。全局商店安装快捷方式与 Python Install Manager 本身不会作为候选，且 FlowLens 在检测与执行期间会禁用管理器的自动运行时安装。如仅安装了管理器，请先手动安装 Python 运行时后重新检测。
- FlowLens 不限定 Python 3 小版本。请使用 **测试** 确认内置 Worker 与 SDK 能在所选解释器中正常启动，并保证脚本语法及第三方包兼容该版本。
- 运行时校验失败时，FlowLens 不会保存启用状态。未安装 Python 或功能关闭时，FlowLens 均可正常启动。

## 五分钟快速上手

在 **设置 > Python** 中配置并启用 Python 后：

1. 打开一个 **HTTP 请求编辑器** 标签页，切换到 **脚本**（**Script**）标签。
2. 粘贴以下脚本并打开脚本开关。
3. 发送请求。
4. 打开响应侧的 **控制台**（**Console**）标签，查看输出的请求 URL 与响应状态码。

```python
from flowlens import *


def onRequest(context, request):
    print(f"request {request.method} {request.url}")
    request.headers.set("X-FlowLens-Script", "enabled")
    return request


def onResponse(context, response):
    print(f"response {response.code}")
    return response
```

该脚本仅影响当前标签页发出的请求，不会影响普通抓包流量。如需在多个请求间复用逻辑，请继续创建全局插件。

## 创建并运行全局插件

1. 打开 **Python 插件** 工作台并新建插件。
2. 编辑 `main.py`，确保同时定义可调用的同步函数 `onRequest` 与 `onResponse`。
3. 在 **匹配规则** 标签中至少添加一条已启用规则。
4. 可选：在 **参数** 标签中填写一个 JSON 对象，供两个 Hook 通过 `context.params` 读取。
5. 保存并校验插件，然后在插件列表中启用它。
6. 在 **HTTP 请求编辑器** 标签页发送前，开启该标签页的全局 Python 插件开关。

**生效条件：**

- HTTP 请求编辑器中的全局插件开关是临时开关，新标签页默认关闭，且不会保存到 API Collection 请求中（仅用于当前标签页绕过控制）。
- 全局插件仅在全部五个条件同时满足时运行：**设置 > Python** 总开关已开启、当前标签页全局插件开关已开启、插件自身开关已开启、插件存在有效 revision、且至少有一条已启用规则匹配。
- 保存或校验会生成不可变 revision。每次发送都会在首个 Hook 执行前快照本次匹配到的插件顺序、revision 和参数，因此请求执行过程中的修改仅影响后续发送。若修改后的插件包校验失败，上一个有效 revision 会继续保持生效。

## 配置插件参数

**参数**（Params）是单个全局插件的配置项，**不是** HTTP 查询参数，也不会自动写入 URL、请求头或请求体；只有当脚本读取 `context.params` 并主动使用时才会生效。

参数编辑器仅接受单个顶层 JSON 对象：

```json
{
  "header_value": "staging",
  "blocked_url_prefix": "https://api.example.com/private/",
  "feature_enabled": true
}
```

在任一 Hook 中读取：

```python
def onRequest(context, request):
    if context.params.get("feature_enabled", False):
        value = str(context.params.get("header_value", "default"))
        request.headers.set("X-Environment", value)
    return request
```

- 参数随全局插件持久化保存，且与其他插件完全隔离。
- 每次请求发送都会获得一份深度只读快照（嵌套对象只读，JSON 数组映射为 Python `tuple`）。
- 保存新参数仅对后续发送生效，不会改变进行中的请求。
- 顶层值必须是 JSON 对象（如 `{}`），数组、字符串、数字、布尔值及 `null` 均会被拒绝。
- 编码后大小上限为 `1 MiB`。
- 当前请求脚本没有参数编辑器，收到的 `context.params` 始终为空对象。

如需在 `onRequest` 与 `onResponse` 之间传递可变状态，请使用 `context.shared` 而非 `context.params`。参数是普通持久化配置而非专用密钥库：FlowLens 不会自动发送或记录它们，但插件代码可以读取、传输或打印，因此只有在完全信任插件及其依赖时才应存放敏感凭据。

## 使用当前请求脚本

在 HTTP 请求编辑器中打开 **脚本** 标签，编辑模板并开启开关：

- 将 HTTP 请求保存或更新到 **API Collections** 时，脚本源码会写入 SQLite；重新打开该请求时会恢复源码，但启用开关始终默认**关闭**，防止已保存代码自动执行。
- 关闭未保存的新标签页会丢弃其脚本。源码不会写入设置、HAR、历史记录或 HBIN，源码修改会计入标签页的未保存状态。
- 与全局插件一致，脚本必须同时提供同步的 `onRequest` 与 `onResponse` 函数。
- 当前请求脚本共享已配置的解释器、Worker 池、Hook 超时、SDK、操作系统权限与可信代码模型，源码大小上限为 `1 MiB`。
- 每次发送都会使用隔离的临时 revision，因此模块全局变量不会在多次发送间保留；跨阶段传递 JSON 状态请使用 `context.shared`。

当全局插件与当前请求脚本同时启用时，Hook 以嵌套方式包裹网络请求：

```text
请求阶段：全局插件 -> 当前请求脚本 -> 网络
响应阶段：网络 -> 当前请求脚本 -> 全局插件
```

当前请求脚本最贴近网络边界。关闭标签页的全局插件开关不会关闭当前请求脚本，需在 **脚本** 标签内单独关闭。

## 在控制台查看脚本输出

响应侧的 **控制台** 标签会在发送期间实时流式展示 `stdout`、`stderr` 与 `context.log` 输出：

- 每条输出通过本次发送的 execution ID 关联，确保不同请求编辑器标签页的输出互不混淆。
- 控制台采用虚拟化日志表格，保留当前标签页最近一次发送的最多 `1,000` 条记录，包含时间、类型、来源与消息内容。
- 点击任意行可在详情窗口查看完整文本，支持换行切换、复制与保存；工具栏还支持复制全部、保存至本地文件和清空当前输出。
- 同一执行链中的全部全局插件与当前请求脚本都会集中展示：全局插件显示插件名称，当前请求脚本显示为 **当前脚本**。
- 每次 `print()` 调用对应一条日志，内容中的换行符会保留在该条记录内部。

## 插件包结构与 Manifest

每个全局插件目录均由 FlowLens 创建并托管：

```text
<plugin-id>/
|-- manifest.json
|-- main.py
`-- helpers.py
```

- `main.py` 为固定入口文件。引用包内其他模块请使用相对导入：

```python
from .helpers import build_token
```

- Worker 会以私有模块名加载 revision。FlowLens 当前维护两个 Worker 进程，因此模块全局变量不保证在多次调用间保留；请求到响应的状态请使用 `context.shared`，需要长期持久化的状态请写入外部存储。
- 无需通过 `pip` 安装 `flowlens` 包；FlowLens 会在运行时向 Worker 进程注入 API v1 `flowlens` 模块。

`manifest.json` 采用严格的 schema v1 校验，拒绝任何未知字段：

```json
{
  "schemaVersion": 1,
  "apiVersion": 1,
  "id": "d9712a7a-5b17-4e6a-a6d2-cb2e8149e734",
  "name": "Example Plugin",
  "description": "Mutates matching request traffic"
}
```

`id` 必须是分配给该插件包的规范小写 UUID，且与托管目录名一致。建议通过 FlowLens 界面修改名称与描述，确保 SQLite 与 `manifest.json` 保持同步。

## 匹配规则与执行顺序

每条规则包含启用开关、HTTP 方法与 URL 通配符模式：

- **HTTP 方法：** 大写精确 HTTP token（如 `GET`、`POST`），或 `*` 匹配所有方法。
- **URL 模式：** 对规范化后的完整 URL 进行匹配。`*` 匹配零个或多个字符，`?` 匹配单个字符（不支持正则表达式）。对于标准绝对 URL 模式，scheme 与 host 不区分大小写，路径与查询参数区分大小写。
- **去重：** 同一插件即便有多条规则同时命中，单次发送也只执行一次。
- **单次快照匹配：** 所有全局插件仅在首个 Hook 执行前针对原始方法和 URL 匹配一次；前置 Hook 修改 URL 不会改变本次已固定的插件列表。
- **执行顺序：** 请求 Hook 按插件列表顺序执行；响应 Hook 按逆序回退，形成中间件式包裹行为。

示例：方法 `GET` 配合 `https://api.example.com/v?/items/*` 可匹配 `https://api.example.com/v1/items/42`。

## Hook 契约

最小可用脚本：

```python
from flowlens import *


def onRequest(context, request):
    return request


def onResponse(context, response):
    return response
```

Hook 必须为同步函数（`def`，禁止 `async def`），且必须返回传入的 SDK 对象或 `None`：

- `onRequest` 返回 `request`：继续传递给下一个 Hook 并最终进入传输层。
- `onRequest` 返回 `None`：在建立网络连接之前阻断请求。
- `onResponse` 返回 `response`：继续执行逆序响应链。
- `onResponse` 返回 `None`：将网络响应标记为已阻断并抑制转换后的结果展示。
- 返回其他类型、抛出未捕获异常、超时、Worker 崩溃或产生无效 HTTP 数据均会导致 Hook 失败。

**失败策略：**

- **请求 Hook 采用 fail-closed：** 不会发送网络请求。
- **响应 Hook 采用 fail-open：** FlowLens 返回未经修改的线上响应并附带插件诊断。响应修改仅影响 HTTP 请求编辑器的结果展示，不会改写捕获到的真实线上数据或传输指标。

## Context API

`context` 提供以下属性：

| 属性 | 类型与语义 |
| --- | --- |
| `id` | 本次请求发送的唯一执行 ID。 |
| `timestamp` | 发送开始时间（Unix 微秒时间戳）。 |
| `original_url` | 任何插件修改前的原始 URL。 |
| `original_method` | 任何插件修改前的原始 HTTP 方法。 |
| `plugin_id`、`plugin_name` | 当前插件的标识与名称。 |
| `params` | 插件 JSON 参数的深度只读快照（JSON 数组映射为元组）。 |
| `transport` | 深度只读传输元数据（`protocol`、`proxy_mode`、`tls_client_hello_profile`、`http2_fingerprint`；不包含代理凭据）。 |
| `shared` | 与当前插件及本次发送绑定的可变 JSON 可序列化对象。 |
| `log` | 结构化日志器，提供 `debug()`、`info()`、`warning()`、`error()` 方法。 |

`context.shared` 在每次 Hook 结束后都会回传 Go 侧，并在响应阶段恢复给同一个插件（即使由另一个 Worker 进程执行）。它必须始终是 JSON 对象且编码大小不超过 `1 MiB`，不同插件或不同发送之间绝不共享状态。

## Request 与 Response API

### `request`

- **可变属性：** `method`、`url`、`path`、`queries`、`headers`、`body`。
- **只读派生视图：** `scheme`、`host`、`port`、`content_type`。
- **URL 联动：** 为 `url` 赋值会重新解析 `path` 与 `queries`；修改 `path` 或 `queries` 会自动重建 `url`。
- **规范化：** 最终请求会重新校验并进入 FlowLens 既有请求管线（URL 路由、自动生成请求头、framing、内容编码、代理、协议、TLS 与 HTTP/2 指纹）。方法必须保持大写，伪请求头、自动生成请求头与 framing 请求头仍由 FlowLens 负责。

### `response` 与 `response.request`

- **`response` 可变属性：** `code`、`headers`、`trailers`、`body`。
- **`response` 只读属性：** `protocol`（上游实际协议，如 `HTTP/1.1`、`HTTP/2.0`）、`status_text`（初始保留上游状态行文本；修改 `code` 时自动更新为对应标准文本）与 `content_type`。
- **`response.request`：** 交给传输层的只读请求语义快照（`method`、`url`、`scheme`、`host`、`port`、`path`、`queries`、`content_type`、`headers`、`body`），不暴露 FlowLens 内部存储字段或托管临时文件路径。

### `Headers`

保留字段顺序、重复名称、原始大小写与空值；按名称查找不区分大小写：

| 方法 | 行为 |
| --- | --- |
| `headers.get(name, default=None)` | 返回首条匹配值，不存在时返回 `default`。 |
| `headers.get_all(name)` | 按字段顺序返回所有匹配值。 |
| `headers.set(name, value)` | 在首条匹配位置替换所有同名字段；不存在时追加。 |
| `headers.add(name, value)` | 追加新字段（允许重复）。 |
| `headers.remove(name)` | 删除全部同名字段。 |
| `headers.clear()` | 清空所有字段。 |

遍历可变 `headers` 产出带可变 `name`/`value` 的 `HeaderField` 对象；遍历 `response.request.headers` 产出 `(name, value)` 元组。

### `Queries`

保留参数顺序、重复名称、空值以及未修改时的原始 URL 查询编码；参数名称区分大小写：

| 方法 | 行为 |
| --- | --- |
| `queries.get(name, default=None)` | 返回首条匹配的解码值，不存在时返回 `default`。 |
| `queries.get_all(name)` | 按顺序返回所有匹配的解码值。 |
| `queries.set(name, value)` | 在首条匹配位置替换所有同名参数；不存在时追加。 |
| `queries.add(name, value)` | 追加新参数（允许重复）。 |
| `queries.remove(name)` | 删除全部同名参数。 |
| `queries.clear()` | 清空所有参数。 |
| `queries.to_string()` | 返回不含前导 `?` 的编码后查询字符串。 |

遍历可变 `queries` 产出 `QueryField` 对象；`response.request.queries` 产出 `(name, value)` 元组且仅提供 `get`、`get_all`、`to_string`。一旦修改 `queries`，FlowLens 会按 Python 标准 URL 编码规则重建查询串（例如空格编码为 `+`）。

### `content_type`

`content_type` 是 `request`、`response` 与 `response.request` 上首条 `Content-Type` 请求/响应头的只读视图。对于非空请求体，FlowLens 会按最终 body kind 自动设置实际发送的 `Content-Type`。如需修改响应的 `Content-Type`，请调用 `response.headers.set("Content-Type", value)` 或 `response.headers.remove("Content-Type")`。

## Body API

`Body` 提供两个只读语义属性：

| 属性 | 语义 |
| --- | --- |
| `kind` | `none`、`text`、`json`、`xml`、`binary`、`file`、`urlencoded`、`multipart` 或 `unavailable`。 |
| `value` | 对应 `kind` 的 Python 值。读取普通内容可能将完整内容物化到 Worker 内存中。 |

各 `kind` 对应的 `value` 类型：

| `kind` | `value` 类型 |
| --- | --- |
| `none` | `None` |
| `text`、`xml` | `str` |
| `json` | 解码后的 JSON 值（通常为 `dict` 或 `list`） |
| `binary` | `bytes` |
| `file` | 只读 `FileDescriptor`（`path`、`name`、`size`） |
| `urlencoded` | 可变 `list[URLEncodedField]` |
| `multipart` | 可变 `list[MultipartPart]` |
| `unavailable` | 读取时抛出 `ValueError` |

- **大内容的透明文件托管：** FlowLens 可能透明地使用托管文件存储较大的 `text`、`json`、`xml` 或 `binary` 内容，这不会改变 `kind`；读取 `value` 仍会返回 `str`、解码后的 JSON 或 `bytes`（并在内存中物化）。`file` kind 则不同：它代表用户选中的上传文件，因此 `value` 始终为 `FileDescriptor`。
- **Multipart 分段：** 每个 `MultipartPart` 拥有可变的 `enabled`、`name`、`value`、`file` 与 `filename` 属性（`filename` 独立于文件描述符的源文件名）。FlowLens 根据 `name` 和 `filename` 生成 `Content-Disposition`（文件 part 未指定 `filename` 时回退为描述符文件名），并为文件 part 设置 `Content-Type: application/octet-stream`。
- **只读请求体快照（`response.request.body`）：** 只读 `BodySnapshot`，语义 `kind` 不变。普通值为不可变快照；`file` 值及 multipart 文件 part 暴露不含路径的 `FileSnapshot`（`name`、`size`）。调用 `response.request.body.write_file(absolute_path)` 可在不接触托管路径的前提下复制可读普通内容或文件体。
- **响应 kind 推断：** 根据 `Content-Type` 与收到的字节推断——有效 JSON 为 `json`；有效 UTF-8 且媒体类型为 `application/xml`、`text/xml` 或 `*+xml` 为 `xml`；二进制媒体类型或无效 UTF-8 为 `binary`；其余有效 UTF-8 为 `text`。

### 直接赋值

可直接将常见 Python 值赋给 `request.body` 或 `response.body`：

```python
request.body = None
request.body = "Hello World"
request.body = b"\x00\x01\x02"
request.body = {"name": "FlowLens", "enabled": True}
request.body = ["one", "two"]
```

| 赋值类型 | 生成的 Body |
| --- | --- |
| `None` | 空 Body（`none`） |
| `str` | UTF-8 文本（`text`） |
| `bytes`、`bytearray` 或 `memoryview` | 二进制 Body（`binary`） |
| `dict` 或 `list` | 紧凑 JSON Body（`json`） |
| `Body` | 显式语义 kind 或复用已有 `Body` 实例 |

其他类型会抛出 `TypeError`。请求规范化阶段会根据最终非空 body kind 生成实际发送的 `Content-Type` 并覆盖原值；清空 body（`None`）不会删除已有 `Content-Type`，必要时请显式移除。

当普通 Python 值无法表达目标 kind 时，请构造显式 `Body`：

```python
request.body = Body("xml", "<root>FlowLens</root>")
request.body = Body(
    "file",
    FileDescriptor.from_file("C:/files/request.bin"),
)
request.body = Body(
    "urlencoded",
    [URLEncodedField("name", "FlowLens")],
)
request.body = Body(
    "multipart",
    [
        MultipartPart("name", "FlowLens"),
        MultipartPart(
            "upload",
            file=FileDescriptor.from_file("C:/files/report.pdf"),
            filename="monthly-report.pdf",
        ),
    ],
)
```

已有 `Body` 的 `kind` 与 `value` 不可直接重新赋值，替换内容请直接对 `request.body` 或 `response.body` 赋新值或新 `Body`（`urlencoded`/`multipart` 列表及元素本身支持原地修改）。

修改 JSON 时请务必重新赋回（大 JSON 可能刚从托管文件物化）：

```python
value = request.body.value
if isinstance(value, str):
    value = json.loads(value)
if isinstance(value, dict):
    value["processed"] = True
    request.body = value
```

### 文件工作流与内存责任

公共 API 不暴露大小阈值或存储模式。读取普通内容的 `value` 可能将完整内容加载进 Python 内存，由此带来的内存占用与 Hook 超时内处理耗时均由脚本作者承担。

如需避免整包加载进内存，可先通过 `write_file(absolute_path)` 导出原始字节，再用 `FileDescriptor.from_file(absolute_path)` 配合显式 kind 替换：

```python
response.body.write_file("C:/response-original.bin")

with open("C:/response-original.bin", "rb") as source, \
        open("C:/response-updated.bin", "wb") as output:
    while chunk := source.read(1024 * 1024):
        output.write(process(chunk))

response.body = Body(
    "binary",
    FileDescriptor.from_file("C:/response-updated.bin"),
)
```

- `FileDescriptor.from_file(path)` 会校验路径并自动读取 `name` 和 `size`，可用于 `Body("file", ...)`（保留语义 `file` kind）或 `MultipartPart`。
- 基于文件描述符的 **请求** 体支持 `text`、`json`、`xml`、`binary` 与 `file`；**响应** 体支持 `text`、`json`、`xml` 与 `binary`（`file` 和 `multipart` 仅支持请求）。`write_file()` 不支持 `urlencoded` 与 `multipart`。
- kind 必须显式声明，不会根据文件扩展名推断；直接赋值路径字符串只会创建包含该路径文本的 `text` body：

```python
request.body = Body("binary", FileDescriptor.from_file("C:/files/request.bin"))
request.body = "C:/files/request.bin"  # 创建内容为该路径字符串的 text body
```

### 临时文件归属与失败行为

- 传给 `FileDescriptor.from_file()` 的路径必须是指向非符号链接普通文件的绝对路径，且构造前需先关闭写入句柄。
- 将 `Body(kind, descriptor)` 赋给请求或响应时，FlowLens 会在赋值返回前把源文件复制到托管临时目录，脚本随后即可删除源文件（在 Windows 上同样安全）。
- 新建的 `MultipartPart` 文件项在请求序列化时才暂存，而非加入列表时暂存；请保持源文件有效直到 `onRequest` 返回。
- 无论成功、阻断、Hook 报错、超时、取消还是 Worker 退出，FlowLens 都会清理托管临时文件。
- 无效请求文件按 fail-closed 失败；无效响应文件按 fail-open 返回未修改的线上响应及诊断信息。
- `unavailable`（SSE）响应体拒绝所有读写，SSE 事件分块绝不会传入 Python。

## 已测试示例

`docs/examples/python-plugins/` 下的全部示例文件均由真实 CPython Worker 集成测试直接执行验证。

### 处理不同 Body Kind

[`docs/examples/python-plugins/body-kinds.py`](../examples/python-plugins/body-kinds.py) 演示了全部请求 body kind 及 SSE 响应的 `unavailable` 状态：

- `none`：赋值 `None` 清空，或赋值其他受支持的 Python 值填充。
- `text`：`value` 为 `str`，直接赋新字符串替换。
- `xml`：`value` 为 `str`，通过 `Body("xml", value)` 或 `Body("xml", FileDescriptor.from_file(absolute_path))` 替换。
- `json`：`value` 为解码后的 JSON，修改后重新赋回。
- `binary`：`value` 为 `bytes`，直接赋字节串或使用 `Body("binary", FileDescriptor.from_file(absolute_path))`。
- `file`：用户选择的上传文件（`value` 为只读 `FileDescriptor`），可通过 `Body("file", FileDescriptor.from_file(absolute_path))` 替换，或用 `write_file()` 不物化地复制字节。
- `urlencoded`：`value` 为可变 `list[URLEncodedField]`，可原地修改或追加 `URLEncodedField("name", "value")`。
- `multipart`：`value` 为可变 `list[MultipartPart]`，可追加 `MultipartPart("name", "value")` 或 `MultipartPart("upload", file=FileDescriptor.from_file(absolute_path), filename="upload.bin")`。已有文件分段暴露只读 `part.file` 与独立可编辑的 `part.filename`。
- `unavailable`：仅见于 SSE 响应，只能在流式开始前修改状态码或响应头。

```python
if request.body.kind == "urlencoded":
    request.body.value.append(URLEncodedField("flowlens", "enabled"))
elif request.body.kind == "multipart":
    request.body.value.append(MultipartPart("flowlens", "enabled"))
    request.body.value.append(
        MultipartPart("upload", file=FileDescriptor.from_file(absolute_path))
    )
```

### 注入请求头、修改 JSON 与使用 `context.shared`

[`docs/examples/python-plugins/header-json-shared.py`](../examples/python-plugins/header-json-shared.py) 演示添加请求头、双阶段修改 JSON 并通过 `context.shared` 传递标记：

```python
import json

from flowlens import *


def _json_object(body):
    value = body.value
    if isinstance(value, str):
        value = json.loads(value)
    return value if isinstance(value, dict) else None


def onRequest(context, request):
    header_value = str(context.params.get("header_value", "enabled"))
    request.headers.add("X-FlowLens-Plugin", header_value)
    if request.body.kind in {"text", "json"}:
        try:
            value = _json_object(request.body)
            if value is not None:
                value["request_plugin"] = True
                request.body = value
        except (TypeError, ValueError, json.JSONDecodeError):
            pass
    context.shared["request_seen"] = True
    context.log.info("request hook completed")
    return request


def onResponse(context, response):
    shared_value = "yes" if context.shared.get("request_seen") else "no"
    response.headers.set("X-FlowLens-Shared", shared_value)
    if response.body.kind in {"text", "json"}:
        try:
            value = _json_object(response.body)
            if value is not None:
                value["response_plugin"] = True
                response.body = value
        except (TypeError, ValueError, json.JSONDecodeError):
            pass
    return response
```

配套参数示例：

```json
{
  "header_value": "documentation-example"
}
```

### 阻断请求

[`docs/examples/python-plugins/block-request.py`](../examples/python-plugins/block-request.py) 在 URL 匹配配置前缀时返回 `None` 阻断请求：

```python
from flowlens import *


def onRequest(context, request):
    blocked_prefix = str(context.params.get("blocked_url_prefix", ""))
    if blocked_prefix and request.url.startswith(blocked_prefix):
        context.log.warning("request blocked by configured URL prefix")
        return None
    return request


def onResponse(context, response):
    return response
```

参数示例：

```json
{
  "blocked_url_prefix": "https://api.example.com/private/"
}
```

### 基于文件处理并替换大 Body

以下节选自 [`docs/examples/python-plugins/large-body-file.py`](../examples/python-plugins/large-body-file.py)，使用 `write_file()` 与分块文件读写处理大负载（完整文件包含 `_new_temp_path` 定义以及空/结构化/不可用 body 的跳过判断）：

```python
from flowlens import *

import hashlib
import os


def onRequest(context, request):
    source_path = _new_temp_path(context.params.get("temp_dir"), ".request")
    try:
        request.body.write_file(source_path)
        digest = hashlib.sha256()
        with open(source_path, "rb") as source:
            while chunk := source.read(1024 * 1024):
                digest.update(chunk)
        request.headers.set("X-Body-SHA256", digest.hexdigest())
    finally:
        os.remove(source_path)
    return request


def onResponse(context, response):
    source_path = _new_temp_path(context.params.get("temp_dir"), ".response")
    replacement_path = _new_temp_path(context.params.get("temp_dir"), ".replacement")
    try:
        response.body.write_file(source_path)
        with open(source_path, "rb") as source, open(replacement_path, "wb") as output:
            output.write(b"processed by FlowLens\n")
            while chunk := source.read(1024 * 1024):
                output.write(chunk)
        response.body = Body("binary", FileDescriptor.from_file(replacement_path))
    finally:
        for path in (source_path, replacement_path):
            try:
                os.remove(path)
            except FileNotFoundError:
                pass
    return response
```

脚本自行创建并清理工作临时文件；将描述符支持的 `Body` 赋回后，FlowLens 会在赋值返回前完成复制。

### 导入第三方软件包

使用 **设置 > Python** 中选择的同一个解释器安装依赖：

```shell
/absolute/path/to/python -m pip install requests
```

随后在插件中直接导入：

```python
from flowlens import *

import requests


def onRequest(context, request):
    request.headers.set("X-Requests-Version", requests.__version__)
    return request


def onResponse(context, response):
    return response
```

API v1 所有插件共用一个配置好的解释器且不提供自动依赖安装。如在已有 Worker 缓存导入后增删或升级了依赖包，请重新加载 Python 运行时或重启 FlowLens。

## 流式响应、大小与失败限制

- **SSE 响应：** `onResponse` 在状态码与响应头到达后、事件流转发开始前触发，此时 `response.body.kind == "unavailable"`。Hook 仅可修改 `code` 与响应头（或返回 `None` 在流式开始前阻断）；事件分块不会进入 Python，修改 trailer 或 body 会被拒绝。
- **内存与暂存：** 读取普通内容的 `body.value` 可能在 Python 内存中完整物化数据。FlowLens 在与 Worker 传输大负载时可能透明使用托管文件，但这并非公开存储模式；需要流式文件处理时请使用 `write_file()`。
- **Worker 协议帧上限：** `64 MiB`。
- **`context.shared` 与参数 JSON 上限：** 各 `1 MiB`。
- **源码大小上限：** 单个托管插件文件 `32 MiB`，完整插件包 `64 MiB`，当前请求脚本 `1 MiB`。
- **Hook 超时：** 可配置范围为 `100–60,000 ms`（默认 `5,000 ms`）。超时、取消、崩溃或破坏协议的 Worker 会被立即终止（Windows 下包含子进程树）并自动替换。
- **同步执行：** 阻塞式文件或网络 I/O 会计入 Hook 超时时间。

## 日志与诊断

- 可使用 `context.log.debug()`、`info()`、`warning()`、`error()`，或标准 `print()` / `sys.stdout.write()`（记为 `info`）与 `sys.stderr.write()`（记为 `error`）。单次写入中的换行符会保留在同一条日志内，不会按行拆分。
- 运行输出仅展示在当前 HTTP 请求编辑器标签页的 **控制台** 中，**Python 插件** 工作台有意不保留独立日志历史。
- **切勿绕过日志包装层：** 不要调用 `os.write(1, ...)`、写入 `sys.__stdout__` 或让子进程继承 Worker 的 `stdout`——向标准输出写入非协议字节会破坏 Worker 通信通道并导致执行失败。直接 `os.write(2, ...)`、`sys.__stderr__` 及子进程继承的 `stderr` 也不会进入控制台，仅留在 Worker 诊断 stderr 中。
- Hook 结果、匹配到的 revision、各阶段耗时、内容转换与脱敏诊断会随 HTTP 请求编辑器结果一并展示。请勿在插件日志中打印凭据或敏感请求体。

## 故障排查

- **无法启用 Python 插件：** 请选择 Python 3 可执行文件（或 Windows Store Python 执行别名）的绝对路径并点击 **测试**。使用虚拟环境时请选择其中的解释器文件，而非虚拟环境目录。若 Store Python 别名失效，请重新安装或重新检测其他运行时。
- **校验提示导入错误：** 请使用 `<所选解释器> -m pip install ...` 安装第三方包；导入插件包内同级文件请使用相对导入（如 `from .helpers import value`）。
- **插件未触发：** 逐项检查五个条件——设置总开关、请求编辑器标签页的全局插件开关、插件自身开关、校验通过标记，以及至少一条匹配原始完整 URL 的已启用规则。
- **参数不生效：** 在 **参数** 编辑器中保存合法的顶层 JSON 对象，并在脚本中通过 `context.params` 显式读取。
- **控制台为空：** 开启对应的全局插件或当前请求脚本后重新发送请求，并确认 Hook 调用了 `print()` 或 `context.log`。控制台仅展示所属标签页最近一次发送的输出。
- **当前请求脚本丢失：** 只有保存到 **API Collections** 的请求会持久化脚本源码，关闭未保存标签页会丢弃脚本；重新打开已保存请求时脚本开关默认关闭。
- **响应体显示 `unavailable`：** 该响应为 SSE，请将 `onResponse` 限制在修改 `code` 与响应头。
- **处理大 Body 内存占用过高：** 避免直接读取 `body.value`，改用 `write_file()` 导出后分块读写文件。
- **`FileDescriptor.from_file()` 或基于文件描述符的 Body 赋值失败：** 请先关闭源文件写入句柄，传入指向非符号链接普通文件的绝对路径，并确保复制操作在 Hook 超时内完成。
- **修改后校验失败：** 上一个有效的不可变 revision 会继续保持激活。修正保存的源码后重新校验；未保存的编辑内容仅保留在当前窗口本地。

插件包导入/导出、自动安装依赖、正则匹配规则、目录监听、异步 Hook 以及单插件独立解释器不属于 API v1 范围。
