# HTTP 请求编辑器 Python 插件

[English](python-plugins.md) | **简体中文**

本文面向希望使用 Python 自定义 HTTP 请求和响应的 FlowLens 用户，介绍环境配置、两种脚本作用域、脚本 API、示例、限制、安全事项和故障排查。

FlowLens 为 HTTP 请求编辑器提供两种 Python 脚本作用域：一种是在 **Python 插件**工作台中管理、通过规则匹配的全局 Python 插件；另一种是绑定当前 HTTP 请求的当前请求脚本。两种脚本都可以在发送前检查或修改请求，也可以检查或修改返回的响应。该功能仅作用于 HTTP 请求编辑器，不会进入普通 MITM 抓包、重发或 WebSocket 客户端；当前请求脚本源码可以随 API Collection 中的 HTTP 请求保存，但不会写入设置、HAR、历史记录或 HBIN 数据。

Python 插件是可选功能，默认关闭。

## 选择脚本作用域

| 作用域 | 适用场景 | 配置能力 | 持久化 |
| --- | --- | --- | --- |
| 全局 Python 插件 | 在多个请求或接口间复用处理逻辑 | 托管文件、匹配规则、插件参数、插件顺序和启用开关 | 由 FlowLens 保存，后续启动仍可使用 |
| 当前请求脚本 | 测试或定制某一个 HTTP 请求 | 一个脚本和标签页内的启用开关；没有匹配规则和可配置参数 | 源码可随 API Collection HTTP 请求保存；启用开关不保存，重新打开时默认关闭 |

两种作用域使用相同的同步 `onRequest` 和 `onResponse` API。如果只是体验脚本功能，建议先使用当前请求脚本；需要复用处理逻辑，或希望按照方法和 URL 自动选择脚本时，再使用全局插件。

## 安全模型

只运行你信任的代码。插件就是普通的本地 Python 代码，拥有与 FlowLens 当前操作系统用户相同的权限，可以读写本地文件、访问网络、启动进程、导入已安装的软件包，也可能通过日志暴露数据。Worker 使用 Python 隔离模式（`-I`）让导入行为更可预测，但这并不是安全沙箱。

启用插件前，请检查插件源码及所有第三方依赖。FlowLens 不会自动安装 Python 软件包。

## 配置 Python

安装 CPython 3.11 或更高版本，或者创建一个专用虚拟环境。独立环境可以避免插件依赖与其他 Python 应用相互影响。

Windows：

```powershell
py -3.11 -m venv C:\venvs\flowlens-plugins
C:\venvs\flowlens-plugins\Scripts\python.exe -m pip install requests
```

macOS 或 Linux：

```shell
python3.11 -m venv "$HOME/.venvs/flowlens-plugins"
"$HOME/.venvs/flowlens-plugins/bin/python" -m pip install requests
```

然后在 FlowLens 中完成配置：

1. 打开 **设置 > Python**。
2. 点击 **检测**，从平台注册信息、`PATH`、当前环境和常见安装位置中选择解释器；也可以手动选择 Python 可执行文件的绝对路径。使用虚拟环境时，Windows 选择其中的 `python.exe`，macOS/Linux 选择 `bin/python`。
3. 点击 **测试**，确认 FlowLens 提示解释器可用。
4. 设置单个 hook 的执行超时。默认值为 5,000 ms，可配置范围为 100–60,000 ms。
5. 启用 Python 插件，并确认仅运行可信代码的安全提示。

检测过程有数量和时间限制，不会扫描整个磁盘。虚拟环境不在当前环境或常见安装位置时，请手动选择。

如果运行时校验失败，FlowLens 不会保存启用状态。没有安装 Python 或该功能保持关闭时，FlowLens 仍能正常启动。

## 五分钟快速体验

在 **设置 > Python** 中配置并启用 Python 后：

1. 打开一个 HTTP 请求编辑器标签页，选择 **脚本**。
2. 粘贴下面的脚本，并打开脚本开关。
3. 发送请求。
4. 打开响应侧的 **控制台** 标签，应当能看到请求 URL 和响应状态。

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

该脚本只修改从当前请求编辑器标签页发出的请求，不会影响普通抓包流量。需要复用这段逻辑时，可以继续按照下面的流程创建全局插件。

## 创建并运行全局插件

1. 打开 **Python 插件**工作台并创建插件。
2. 编辑 `main.py`。必须同时提供 `onRequest` 和 `onResponse`，两者都必须是可调用的同步函数。
3. 在 **匹配规则**标签中至少添加一条已启用规则。
4. 可以在 **参数**标签中填写一个 JSON 对象，两个 hook 都能通过 `context.params` 读取。
5. 保存并校验插件，然后在插件列表中启用它。
6. 打开 HTTP 请求编辑器标签页，发送时保持全局 Python 插件开关开启。

HTTP 请求编辑器中的全局插件开关是临时开关，新标签页默认关闭。它只用于控制当前标签页是否绕过全局插件，不会保存到 API Collection 请求。只有设置中的 Python 插件总开关、当前标签页的全局插件开关、插件自身开关、校验状态和至少一条匹配规则全部允许时，全局插件才会执行。

保存或校验会创建不可变 revision。每次请求发送都会在第一个 hook 执行前固定本次匹配到的插件顺序、revision 和参数，因此请求执行期间的编辑只影响后续发送。如果修改后的插件包校验失败，上一个可用 revision 会继续保持激活。

## 配置插件参数

**参数**是某一个全局插件的配置值。它不是 HTTP Query 参数，也不会自动加入 URL、请求头或请求体。只有脚本读取 `context.params`，并使用其中的值修改或阻断请求时，这些参数才会影响请求。

参数编辑器只接受一个 JSON 对象，例如：

```json
{
  "header_value": "staging",
  "blocked_url_prefix": "https://api.example.com/private/",
  "feature_enabled": true
}
```

两个 hook 都可以读取这些值：

```python
def onRequest(context, request):
    if context.params.get("feature_enabled", False):
        value = str(context.params.get("header_value", "default"))
        request.headers.set("X-Environment", value)
    return request
```

- 参数随全局插件一起保存，并与其他插件相互隔离。
- 每次请求发送都会获得一份深度只读快照。嵌套对象同样只读，JSON 数组在 Python 中表现为元组。
- 保存新参数只影响后续发送，不会改变已经在执行的请求。
- 顶层值必须是 `{}` 这类 JSON 对象，不能是数组、字符串、数字、布尔值或 `null`。
- 编码后的参数对象最大为 1 MiB。
- 当前请求脚本没有参数编辑器，收到的 `context.params` 是空对象。

如果需要在 `onRequest` 和 `onResponse` 之间传递可变状态，应使用 `context.shared`，而不是 `context.params`。参数是持久化配置，并不是专门的密钥存储。FlowLens 不会自动发送或打印这些参数，但插件代码可以读取、传输或输出它们，因此只有在信任完整脚本及其依赖时才应存放凭据。

## 使用当前请求脚本

如果只需要处理一个请求，可以打开 HTTP 请求编辑器内的 **脚本**标签，编辑模板并启用脚本。将 HTTP 请求保存或更新到 API Collection 时，脚本源码会一起写入 SQLite；重新打开该请求会恢复源码，但启用开关始终默认关闭，避免持久化代码自动执行。未保存到 API Collection 的新标签页在关闭后仍会丢失脚本，源码也不会写入设置、HAR、历史记录或 HBIN。源码修改会计入标签页的未保存状态。和全局插件一样，脚本也必须同时提供同步的 `onRequest` 与 `onResponse` hook。

当前请求脚本与全局插件共享已配置的解释器、Worker 池、hook 超时、SDK、权限和可信代码安全模型。源码大小上限为 1 MiB。每次发送都会使用独立的临时 revision，因此模块全局变量不会复用于之后的发送；需要把 JSON 状态从请求 hook 传递到响应 hook 时，请使用 `context.shared`。

全局插件和当前请求脚本同时启用时，请求 hook 按以下顺序执行：

```text
全局插件 -> 当前请求脚本 -> 网络
```

响应 hook 按相反顺序回退：

```text
网络 -> 当前请求脚本 -> 全局插件
```

因此，当前请求脚本最靠近网络边界。关闭当前请求的全局插件开关不会关闭当前请求脚本；如需关闭当前请求脚本，请使用 **脚本**标签内的开关。

## 在控制台查看脚本输出

响应侧的 **控制台**标签会在本次发送执行期间实时显示 `stdout`、`stderr` 和 `context.log` 输出。日志通过本次发送的 execution ID 关联，因此不会混入其他请求编辑器标签页的输出。

控制台使用只读 Monaco 编辑器展示当前标签页最近一次发送的最多 1,000 条输出。默认开启自动换行并可通过工具栏切换；工具栏还支持复制全部输出、保存到本地文件和清空当前输出。同一条执行链中的所有全局插件和当前请求脚本都会显示在这里，并以全局插件 ID 或 **当前请求脚本**标签区分来源。

## 插件包结构与 manifest

每个插件目录由 FlowLens 创建和管理：

```text
<plugin-id>/
|-- manifest.json
|-- main.py
`-- helpers.py
```

`main.py` 是固定入口文件。导入插件包内的其他文件时应使用相对导入，例如：

```python
from .helpers import build_token
```

Worker 使用私有模块名加载 revision。FlowLens 当前使用两个 Worker 进程，因此不能保证模块全局变量在不同调用之间保留。请求与响应之间的状态请使用 `context.shared`，确实需要长期保存的数据应写入外部存储。

Manifest schema v1 使用严格校验，不允许未知字段：

```json
{
  "schemaVersion": 1,
  "apiVersion": 1,
  "id": "d9712a7a-5b17-4e6a-a6d2-cb2e8149e734",
  "name": "Example Plugin",
  "description": "Mutates matching request traffic"
}
```

`id` 必须是插件包分配到的规范小写 UUID，并且与其托管目录一致。建议通过 FlowLens 界面修改名称和描述，以保证数据库与 manifest 一致。

不需要通过 pip 单独安装 `flowlens` 软件包。FlowLens 会向 Worker 进程提供 API v1 模块。

## 匹配规则与执行顺序

每条规则包含启用状态、HTTP 方法和 URL 通配符：

- 方法必须是 `GET`、`POST` 等大写 HTTP token，或使用 `*` 匹配所有方法。
- URL 使用规范化后的完整 URL 进行匹配。`*` 匹配零个或多个字符，`?` 只匹配一个字符；不支持正则表达式。
- 对于普通绝对 URL 模式，scheme 和 host 不区分大小写，路径和查询参数仍区分大小写。
- 同一个插件即使有多条规则同时命中，也只执行一次。
- 所有插件只按照原始 HTTP 方法和 URL 匹配一次。前一个 hook 修改 URL 不会改变已经固定的插件集合。
- 请求 hook 按插件列表顺序执行；响应 hook 按相反顺序执行，从而形成类似包装器的行为。

例如，方法 `GET` 配合 `https://api.example.com/v?/items/*`，可以匹配 `https://api.example.com/v1/items/42`。

## Hook 契约

最小插件如下：

```python
from flowlens import *


def onRequest(context, request):
    return request


def onResponse(context, response):
    return response
```

Hook 不能使用 `async def`，并且必须返回收到的同一个 SDK 对象或 `None`：

- `onRequest` 返回 `Request` 时，会继续执行下一个插件，最后进入传输流程。
- `onRequest` 返回 `None` 时，会在建立网络连接之前阻断请求。
- `onResponse` 返回 `Response` 时，会继续执行反向响应链。
- `onResponse` 返回 `None` 时，会将网络响应标记为已阻断，并抑制转换后的结果展示。
- 返回其他类型、抛出异常、执行超时、Worker 崩溃或产生无效 HTTP 数据都会使 hook 失败。

请求 hook 失败时采用 fail-closed：不会发送请求。响应 hook 失败时采用 fail-open：FlowLens 返回未经插件修改的线上响应，并附带插件诊断。响应修改只改变 HTTP 请求编辑器结果的展示，不会改写抓取到的线上真实数据或传输指标。

## Context API

`context` 提供以下属性：

| 属性 | 类型与语义 |
| --- | --- |
| `id` | 请求执行 ID。 |
| `timestamp` | 发送开始时间，Unix 微秒时间戳。 |
| `original_url` | 任何插件修改之前的 URL。 |
| `original_method` | 任何插件修改之前的方法。 |
| `plugin_id`、`plugin_name` | 当前插件的标识和名称。 |
| `params` | 插件 JSON 参数的深度只读值；JSON 数组表现为元组。 |
| `transport` | 深度只读的传输元数据。 |
| `shared` | 可修改、可 JSON 序列化，并与当前插件及本次请求发送隔离的对象。 |
| `log` | 提供 `debug`、`info`、`warning` 和 `error` 方法。 |

`context.transport` 包含 `protocol`、`proxy_mode`、`tls_client_hello_profile` 和 `http2_fingerprint`，不会暴露代理凭据。

每个 hook 执行后，`context.shared` 都会返回 Go 侧，并在响应阶段重新传给同一个插件，即使响应 hook 由另一个 Worker 执行也是如此。它必须始终是 JSON 对象，编码后不能超过 1 MiB。不同插件或不同发送之间不会共享状态。

## Request 与 Response API

`request` 提供可修改的 `method`、`url`、`path`、`queries`、`headers` 和 `body` 属性。`scheme`、`host`、`port` 和 `content_type` 是根据当前 URL 与 Header 派生的只读视图。给 `url` 赋值会重新解析 `path` 与 `queries`；修改 `path` 或 `queries` 会重建 `url`。最终结果会重新校验，然后进入既有请求 URL、自动生成请求头、framing、内容编码、代理、协议、TLS 和指纹处理流程。HTTP 方法必须保持大写，自动生成请求头、伪请求头和 framing 请求头仍由 FlowLens 负责。

`response` 提供可修改的 `code`、`headers`、`trailers` 和 `body` 属性。`protocol` 是上游响应实际使用的协议，例如 `HTTP/1.1` 或 `HTTP/2.0`，并且只读。`status_text` 初始保留上游状态行文本，同样只读；给 `code` 赋予不同值时，它会更新为新状态码对应的标准文本。`content_type` 同样只读。

`response.request` 是交给传输层的请求只读语义快照，提供 `method`、`url`、`scheme`、`host`、`port`、`path`、`queries`、`content_type`、`headers` 和 `body`。该快照不会暴露 FlowLens 的 Body 存储字段或托管临时文件路径。

`Headers` 会保留字段顺序、重复字段名、原始大小写和空值，按名称查找时不区分大小写：

| 操作 | 行为 |
| --- | --- |
| `headers.get(name, default=None)` | 返回第一条匹配值。 |
| `headers.get_all(name)` | 按字段顺序返回所有匹配值。 |
| `headers.set(name, value)` | 在第一条匹配位置替换全部同名字段；不存在时追加。 |
| `headers.add(name, value)` | 追加一个字段，可以形成重复字段。 |
| `headers.remove(name)` | 删除全部同名字段。 |
| `headers.clear()` | 删除所有字段。 |

遍历时会得到 `HeaderField` 对象，其中 `name` 和 `value` 均可修改；请求只读快照则会产生 `(name, value)` 元组。

`Queries` 会保留字段顺序、重复名称、空值，以及未修改 URL 的原始 query 编码；查询参数名称区分大小写：

| 操作 | 行为 |
| --- | --- |
| `queries.get(name, default=None)` | 返回第一条匹配的解码后值。 |
| `queries.get_all(name)` | 按顺序返回所有匹配的解码后值。 |
| `queries.set(name, value)` | 在第一条匹配位置替换全部同名参数；不存在时追加。 |
| `queries.add(name, value)` | 追加一个参数，可以形成重复参数。 |
| `queries.remove(name)` | 删除全部同名参数。 |
| `queries.clear()` | 删除所有参数。 |
| `queries.to_string()` | 返回不包含开头 `?` 的 query 字符串。 |

遍历可修改的 queries 会得到 `QueryField` 对象；请求只读快照会产生 `(name, value)` 元组，并且只提供 `get`、`get_all` 与 `to_string`。一旦修改 queries，FlowLens 会使用 Python 标准 URL 编码规则重建 query，例如空格会编码为 `+`。

`content_type` 是 `request`、`response` 和 `response.request` 上第一条 `Content-Type` Header 的只读视图。请求 Body 非空时，FlowLens 会根据最终 Body kind 替换实际发送值。需要修改响应 Content-Type 时，使用 `response.headers.set("Content-Type", value)` 或 `response.headers.remove("Content-Type")`。

## Body API

`Body` 提供两个只读语义属性：

| 成员 | 语义 |
| --- | --- |
| `kind` | `none`、`text`、`json`、`xml`、`binary`、`file`、`urlencoded`、`multipart` 或 `unavailable`。 |
| `value` | 与 `kind` 对应的 Python 值。读取普通内容时可能会在 Worker 内存中完整物化 Body。 |

各 kind 的 value 类型如下：

| `kind` | `value` |
| --- | --- |
| `none` | `None` |
| `text`、`xml` | `str` |
| `json` | 解码后的 JSON 值，通常是 `dict` 或 `list` |
| `binary` | `bytes` |
| `file` | 只读 `FileDescriptor`，包含 `path`、`name` 和 `size` |
| `urlencoded` | 可修改的 `list[URLEncodedField]` |
| `multipart` | 可修改的 `list[MultipartPart]` |
| `unavailable` | 读取时抛出 `ValueError` |

FlowLens 可能透明地用托管文件保存较大的文本、JSON、XML 或二进制内容。这不会改变 `kind`；读取 `value` 仍然得到语义上的 `str`、JSON 值或 `bytes`，但可能把全部内容载入内存。`file` kind 不同：它表示用户选择的请求文件，因此 value 保持为 `FileDescriptor`。

每个 `MultipartPart` 都提供可修改的 `enabled`、`name`、`value`、`file` 和 `filename`。`filename` 与 descriptor 的源文件名相互独立。FlowLens 会根据 `name` 和 `filename` 自动生成每个 part 的 `Content-Disposition`，文件 part 未设置 `filename` 时回退到 descriptor 文件名。文件 part 的 `Content-Type` 使用 `application/octet-stream`。

`response.request.body` 是只读的 `BodySnapshot`，其中 `kind` 保持相同的内容语义。普通 value 是不可修改的快照；`file` value 是只提供 `name` 和 `size` 的 `FileSnapshot`，multipart 文件 part 也使用同样的不含路径快照。可以调用 `response.request.body.write_file(绝对路径)` 复制可读取的普通 Body 或文件 Body，无需接触托管存储路径。与 `Body` 一样，读取较大的普通快照 value 可能会在 Worker 内存中完整物化内容。

响应 kind 根据 `Content-Type` 和接收到的字节推断。有效 JSON 响应使用 `json`；有效 UTF-8 且媒体类型为 `application/xml`、`text/xml` 或以 `+xml` 结尾的响应使用 `xml`；二进制媒体类型或无效 UTF-8 使用 `binary`；其他 UTF-8 响应使用 `text`。

### 直接赋值

请求和响应 Body 都可以直接接收常见 Python 值：

```python
request.body = None
request.body = "Hello World"
request.body = b"\x00\x01\x02"
request.body = {"name": "FlowLens", "enabled": True}
request.body = ["one", "two"]
```

| 赋值 | 结果 |
| --- | --- |
| `None` | 空 Body |
| `str` | UTF-8 文本 |
| `bytes`、`bytearray` 或 `memoryview` | 二进制 Body |
| `dict` 或 `list` | 紧凑 JSON Body |
| `Body` | 指定明确的语义 kind，或复用已有 Body |

其他类型抛出 `TypeError`。请求规范化阶段会根据最终的非空 Body kind 生成实际发送的 `Content-Type`，并覆盖已有值。清空 Body 不会删除已有的 `Content-Type`；需要时应由脚本显式移除该 Header。

当普通 Python 值无法表达目标 kind 时，使用显式 `Body`：

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

不能给已有 Body 的 `kind` 和 `value` 重新赋值。需要替换内容时，应把新值或新 `Body` 赋给 `request.body` 或 `response.body`。结构化项目对象及其列表仍然可以修改。

处理 JSON 时，先读取 value，修改后再赋回。重新赋值很重要，因为较大的 JSON Body 可能刚从托管文件中物化：

```python
value = request.body.value
if isinstance(value, str):
    value = json.loads(value)
if isinstance(value, dict):
    value["processed"] = True
    request.body = value
```

### 文件工作流与内存责任

公共 API 没有 Body 大小阈值或存储模式。读取普通内容的 `value` 可能把完整 Body 载入 Python 内存。脚本作者需要自行承担这部分内存开销以及 hook 超时内的处理时间。

可使用 `write_file(path)` 把原始 Body 字节复制到绝对目标路径而不先完整物化。需要从本地文件替换 Body 时，构造 `FileDescriptor` 并显式指定语义 kind：

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

descriptor-backed 请求 Body 支持 `text`、`json`、`xml`、`binary` 和 `file`；descriptor-backed 响应 Body 支持 `text`、`json`、`xml` 和 `binary`，`file` 仍仅支持请求。`write_file()` 不支持 URL-encoded 和 multipart Body。

请求需要保留语义上的 `file` kind 时，使用 `FileDescriptor.from_file(path)`；它会校验路径，并自动读取文件名和大小：

```python
request.body = Body(
    "file",
    FileDescriptor.from_file("C:/files/request.bin"),
)
```

新的 multipart 文件 part 也使用同一个描述符构造方法：

```python
request.body = Body(
    "multipart",
    [
        MultipartPart("description", "monthly report"),
        MultipartPart(
            "upload",
            file=FileDescriptor.from_file("C:/files/report.pdf"),
            filename="monthly-report.pdf",
        ),
    ],
)
```

源路径必须是绝对路径，并指向非符号链接普通文件。把 descriptor-backed `Body` 赋给 `request.body` 或 `response.body` 时，FlowLens 会立即把源文件复制到托管临时存储；赋值成功后脚本可以删除源文件，在 Windows 上同样适用。`file` 和 multipart 都仅支持请求 Body。

kind 必须显式指定，不会根据扩展名推断。直接赋值路径字符串只会创建文本 Body，不会读取文件：

```python
request.body = Body("binary", FileDescriptor.from_file("C:/files/request.bin"))
request.body = "C:/files/request.bin"  # 内容为路径的 text
```

较大的内联替换会在进入 IPC 前自动暂存。4 MiB 阈值和托管文件位置属于内部实现，不是 Python API。

### 临时文件归属与失败行为

- 传给 `FileDescriptor.from_file()` 的路径必须是绝对路径、非符号链接普通文件；构造 descriptor 前先关闭写入句柄。
- 把 `Body(kind, descriptor)` 赋给请求或响应时，会在赋值返回前把待处理源文件复制到当前 FlowLens session；之后脚本可以删除源文件。
- 新建 multipart 文件 part 是可修改列表项，因此会在请求序列化时暂存，而不是在 owner 赋值时暂存；不能在 hook 返回前删除或替换源文件。
- 成功、阻断、hook 失败、超时、取消或 Worker 退出后，FlowLens 都会清理托管文件。
- 无效请求文件按 fail-closed 处理；无效响应文件按 fail-open 处理，并返回未修改的响应和诊断。
- `unavailable` 或正在流式传输的 SSE Body 会拒绝读取和修改，SSE 事件块不会进入 Python。

## 已测试示例

以下文件可以直接复制使用，并且会由真实 CPython Worker 集成测试执行。

### 处理不同 Body kind

[`docs/examples/python-plugins/body-kinds.py`](../examples/python-plugins/body-kinds.py) 演示了所有请求 Body kind，以及 SSE 响应的 `unavailable` 状态。主要用法如下：

- `none`：赋值 `None` 可清空，也可以赋其他普通 Python 值进行替换。
- `text`：`value` 是 `str`；把替换字符串赋给 `request.body` 或 `response.body`。
- `xml`：`value` 是 `str`；内联 XML 使用 `Body("xml", value)`，文件来源 XML 使用 `Body("xml", FileDescriptor.from_file(absolute_path))`。
- `json`：`value` 是解码后的 JSON；修改后再赋回所属 Body。
- `binary`：`value` 是 `bytes`；可以直接赋 bytes，或使用 `Body("binary", FileDescriptor.from_file(absolute_path))`。
- `file`：该请求语义 kind 表示用户选择的文件。`value` 是只读 `FileDescriptor`；可通过 `Body("file", FileDescriptor.from_file(absolute_path))` 创建替换，也可以用 `write_file()` 在不物化内容的情况下复制既有字节。
- `urlencoded`：`body.value` 是可修改的 `list[URLEncodedField]`，可以修改字段或追加 `URLEncodedField("name", "value")`。
- `multipart`：`body.value` 是可修改的 `list[MultipartPart]`；文本 part 使用 `MultipartPart("name", "value")`，文件 part 使用 `MultipartPart("upload", file=FileDescriptor.from_file(absolute_path), filename="upload.bin")`；已有上传文件通过只读 `part.file` 描述符提供，`part.filename` 可独立修改。
- `unavailable`：只会出现在 SSE 响应中。不要访问或替换 Body；只能在流式转发开始前修改状态码或响应头。

例如，结构化请求 Body 应按对象修改，而不是当作原始字节读取：

```python
if request.body.kind == "urlencoded":
    request.body.value.append(URLEncodedField("flowlens", "enabled"))
elif request.body.kind == "multipart":
    request.body.value.append(MultipartPart("flowlens", "enabled"))
    request.body.value.append(
        MultipartPart("upload", file=FileDescriptor.from_file(absolute_path))
    )
```

FlowLens 的内部传输表示不会改变 `kind`，并且不会暴露给脚本。完整示例覆盖全部既有 kind，并将 `unavailable` 的处理限制在响应头。

### 添加请求头、修改 JSON 与使用 `context.shared`

[`docs/examples/python-plugins/header-json-shared.py`](../examples/python-plugins/header-json-shared.py) 会添加请求头，在两个阶段修改 JSON，并把一个标记传递给响应 hook：

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

配套参数对象：

```json
{
  "header_value": "documentation-example"
}
```

### 阻断请求

[`docs/examples/python-plugins/block-request.py`](../examples/python-plugins/block-request.py) 会针对配置的 URL 前缀返回 `None`：

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

### 使用文件处理并替换大 Body

[`docs/examples/python-plugins/large-body-file.py`](../examples/python-plugins/large-body-file.py) 会先调用 `write_file()`，再通过普通 Python 文件 API 处理请求与响应 Body：

```python
import hashlib
import os
import tempfile


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

完整示例会跳过空、结构化和不可用 Body，并由真实 CPython 集成测试执行。脚本自行创建和删除工作文件；descriptor-backed Body 赋值会在返回前复制完成的替换文件。

### 导入第三方软件包

使用设置中选择的同一个解释器安装依赖：

```shell
/absolute/path/to/python -m pip install requests
```

然后使用 [`docs/examples/python-plugins/third-party-package.py`](../examples/python-plugins/third-party-package.py)：

```python
from flowlens import *

import requests


def onRequest(context, request):
    request.headers.set("X-Requests-Version", requests.__version__)
    return request


def onResponse(context, response):
    return response
```

FlowLens 不提供自动依赖安装功能，API v1 中的所有插件共用一个配置好的解释器。如果修改软件包时已有 Worker 缓存了相关导入，请重新加载 Python 运行时或重启 FlowLens。

## 流式响应、大小与失败限制

- SSE 响应 hook 会在状态码和响应头到达后、FlowLens 开始转发事件前运行。此时 `response.body.kind` 为 `unavailable`。
- SSE hook 只能修改状态码和响应头。事件数据块不会经过 Python，修改响应 Trailer 或 Body 会被拒绝。返回 `None` 会在开始流式传输前阻断响应。
- 读取普通内容的 `body.value` 可能把完整 Body 物化到 Python 内存中；由脚本作者承担相应的内存占用和 hook 执行时间。
- FlowLens 在与 Worker 传递 Body 时可能透明使用托管文件，但这不是公开的存储模式。需要基于文件处理时，请使用 `write_file()` 和普通 Python 文件 API。
- Worker 协议帧最大为 64 MiB。
- `context.shared` 和参数 JSON 对象的大小上限均为 1 MiB。
- 单个托管源码文件最大为 32 MiB，完整插件包最大为 64 MiB。
- 每个 hook 使用设置中配置的 100–60,000 ms 超时。超时、取消、崩溃或破坏协议的 Worker 会被终止并替换。
- Hook 同步执行，阻塞式文件或网络 I/O 会占用 hook 超时时间。

## 日志与诊断

可以使用 `context.log.debug()`、`info()`、`warning()` 或 `error()`。`print()` 会作为来自 `stdout` 的 `info` 日志捕获，写入 `stderr` 的内容会作为 `error` 捕获。本次执行的输出显示在 HTTP 请求编辑器的 **控制台**标签中。Python 插件工作台有意不保留另一份日志历史：规则、参数、文件和校验状态留在插件工作台，运行输出则跟随产生它的请求执行。

Hook 结果、匹配到的 revision、各阶段耗时、内容转换和脱敏后的诊断会显示在 HTTP 请求编辑器结果中。插件自身的日志可能包含敏感数据，因此不要输出凭据或完整请求体。

## 故障排查

- **无法启用 Python 插件：** 请选择 Python 3.11+ 常规可执行文件的绝对路径并点击 **测试**。使用虚拟环境时，应选择该环境中的解释器，而不是目录。
- **校验提示导入失败：** 使用 `selected-python -m pip install ...` 安装依赖。导入插件包内文件时，使用 `from .helpers import value` 这类相对导入。
- **插件没有执行：** 检查设置中的 Python 插件总开关、HTTP 请求编辑器标签页的全局插件开关、插件自身开关、校验标记，以及是否至少有一条已启用规则。规则使用原始完整 URL 进行匹配。
- **参数没有生效：** 保存一个顶层 JSON 对象，然后在插件中通过 `context.params` 读取。参数只是配置，不会自动加入 HTTP 请求。
- **控制台没有输出：** 启用对应的全局插件或当前请求脚本后重新发送，并确认 hook 调用了 `print()` 或 `context.log`。控制台只显示所属请求编辑器标签页最近一次发送的输出。
- **当前请求脚本消失：** 只有保存或更新到 API Collection 的 HTTP 请求会持久化脚本源码；未保存的新标签页关闭后会丢失。重新打开已保存请求时脚本默认关闭，需要手动启用。需要跨多个请求复用时请创建全局插件。
- **响应 Body 不可用：** 该响应是 SSE。请将该 hook 限制为修改 `code` 和响应头；普通响应仍可通过 Body API 访问。
- **处理 Body 时内存占用过高：** 不要读取该 Body 的 `body.value`；请先通过 `write_file()` 导出，再使用普通 Python 文件 API 处理。
- **`file()`、`binary(path)` 或 `textFromFile()` 失败：** 请先关闭源文件写入句柄，传入非符号链接普通文件的绝对路径，并确保复制操作在 hook 超时内完成。
- **编辑内容校验失败：** 上一个可用的不可变 revision 会继续激活。修正并保存源码后重新校验；未保存的编辑器内容只保留在当前 FlowLens 窗口中。

插件包导入/导出、自动安装依赖、正则规则、目录监听、异步 hook 和每插件独立解释器不属于 API v1。
