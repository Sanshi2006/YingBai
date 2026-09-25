# 智能客服底座 MVP

面向纺织品、鞋类和消费品检测实验室的移动端智能客服底座。当前已完成移动端与 Gin 后端联通，以及知识文档上传、文本提取、语义切片、千问向量化、pgvector 持久化、Top-K 向量检索、完整 RAG 回答和 SQLite 对话日志。

## 当前能力

- 适配 375～430 像素宽度的移动端 H5；
- 外部客户、客服人员、管理员三种演示身份；
- 完整聊天界面、消息气泡、时间、发送中状态和失败重试；
- `GET /health` 健康检查；
- `POST /chat` 完整 RAG 问答接口，支持问题改写、权限检索、阈值拒答、知识回答和引用；
- `POST /api/v1/documents` 文档上传接口，支持 PDF、TXT、MD 和业务元数据；
- `GET /api/v1/documents` 分页文档列表，支持文件名、品类、类型、权限和状态筛选；
- 同源 `/documents` 文档管理页，支持上传、删除、失败重试和重新向量化；
- 使用原始文件 SHA-256 去重，并持久化安全处理失败原因；
- PDF、TXT、MD 文本提取与提取状态记录；
- 按标题、段落和句末标点进行语义边界切片；
- 文档元数据、文本分块和 1024 维向量通过 PostgreSQL/pgvector 事务保存；
- 基于用户问题生成查询向量，按余弦相似度执行权限过滤后的 Top-K 检索；
- 提供 `LLMProvider` 抽象、OpenAI Compatible 向量实现和确定性 Fake 实现；
- 提供独立的 `ChatLLMProvider`，支持问题改写及基于授权知识片段构造回答 Prompt；
- 检索为空或相似度低于阈值时固定回复“知识库暂无依据，请转人工”，不调用最终回答模型；
- 成功完成的知识回答和固定拒答会写入嵌入式 SQLite，包含问题、答案、引用文件、角色、会话和时间；
- Gin 同源托管移动端页面，无需单独安装或启动前端工具；
- 同一局域网内可通过手机浏览器或微信内置浏览器访问。

当前角色入口是演示级身份选择，不是生产级登录认证。完整 RAG 链路已接入 `/chat`，移动端可展开查看引用文件名；多轮记忆和 LIMS Mock 将在后续阶段接入。

## 环境要求

- Windows 10/11；
- Go 1.27 或兼容版本；
- Docker Desktop，且 Docker Engine 正在运行；
- 电脑和手机接入同一个局域网时，可进行真机访问。

本阶段移动端使用原生 HTML、CSS 和 JavaScript，不需要 Node.js 或 npm。

## 项目结构

```text
ProjectForYingBai/
├─ mobile/                 # 移动端 H5 页面与静态资源
│  ├─ assets/
│  │  ├─ app.js
│  │  ├─ documents.js
│  │  ├─ documents.css
│  │  └─ styles.css
│  ├─ documents.html       # 文档管理页面
│  └─ index.html           # 移动端客服页面
├─ backend/                # Gin 后端
│  ├─ cmd/server/main.go   # 服务启动入口
│  ├─ internal/chunker/    # 语义边界切片
│  ├─ internal/config/     # .env 与环境变量配置
│  ├─ internal/database/   # PostgreSQL、SQLite 连接与版本化迁移
│  ├─ internal/handler/    # HTTP 接口
│  ├─ internal/llm/        # OpenAI Compatible 与 Fake Provider
│  ├─ internal/repository/ # PostgreSQL 数据访问
│  ├─ internal/router/     # 路由与测试
│  ├─ go.mod
│  └─ go.sum
├─ data/knowledge/         # 后续知识文档目录
├─ data/runtime/           # SQLite 运行文件（Git 忽略）
├─ docs/CONSTRAINTS.md     # 工程约束文件
├─ docker-compose.yml      # PostgreSQL 本地运行配置
├─ .env.example            # 环境变量示例
├─ target.md               # 本周任务书
└─ request.md              # 整体解决方案
```

## 启动项目

先在项目根目录启动 PostgreSQL：

```powershell
cd D:\ProjectForYingBai
docker compose up -d --wait postgres
```

该服务使用 `pgvector/pgvector:0.8.6-pg18-trixie`。看到容器状态为 `Healthy` 后，再启动后端：

```powershell
cd D:\ProjectForYingBai\backend
go mod download
go run ./cmd/server
```

看到以下内容代表启动成功：

```text
backend listening on http://0.0.0.0:8080
```

现在可以在电脑浏览器打开：

```text
http://127.0.0.1:8080
```

页面和接口由同一个 Gin 服务提供，因此不需要再打开第二个前端服务。

文档管理页面地址：

```text
http://127.0.0.1:8080/documents
```

停止服务时，在运行服务的 PowerShell 窗口按 `Ctrl+C`。

PostgreSQL 数据保存在 Docker 卷中，停止容器不会丢失。只停止数据库时执行：

```powershell
cd D:\ProjectForYingBai
docker compose stop postgres
```

### 配置

默认开发连接为：

```text
postgres://yingbai:yingbai_dev_password@localhost:5432/yingbai?sslmode=disable
```

项目根目录已创建本地 `.env`，后端从 `backend/` 启动时会自动加载它；进程中已经显式设置的环境变量优先级更高，不会被 `.env` 覆盖。`.env` 已被 Git 忽略，仓库只提交 `.env.example`。

知识库相关变量包括 `DATABASE_URL`、`POSTGRES_DB`、`POSTGRES_USER`、`POSTGRES_PASSWORD` 和 `POSTGRES_PORT`。示例密码只用于本地开发；共享或生产环境必须替换。后端启动时会自动执行 `backend/internal/database/migrations/` 中的 PostgreSQL SQL 迁移。

对话日志使用纯 Go 嵌入式 SQLite 驱动，不需要安装 SQLite，也不需要新增 Docker 容器。默认数据库文件为 `data/runtime/conversations.db`，可以通过 `SQLITE_DATABASE_PATH` 修改；启动时会自动创建目录、启用 WAL，并执行 `backend/internal/database/sqlite_migrations/` 中的版本化迁移。该运行目录和 `*.db`、`*.db-wal`、`*.db-shm` 已被 Git 忽略。

向量 Provider 使用以下变量：

| 变量 | 说明 | 示例 |
| --- | --- | --- |
| `LLM_API_KEY` | 千问/百炼服务密钥 | `replace_with_your_dashscope_api_key` |
| `LLM_BASE_URL` | OpenAI 兼容 API 根地址，代码会追加 `/embeddings` | `https://your-workspace-id.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` |
| `LLM_EMBEDDING_MODEL` | 千问向量模型名称 | `qwen3.7-text-embedding` |
| `RAG_TOP_K` | 每次检索最多返回的知识分块数，允许 1～20，未设置时默认为 5 | `5` |
| `RAG_SIMILARITY_THRESHOLD` | 进入回答 Prompt 的最低余弦相似度，允许 0～1 | `0.55` |

问题改写及后续回答生成使用单独的 Chat Provider：

| 变量 | 说明 | DeepSeek 示例 |
| --- | --- | --- |
| `LLM_CHAT_API_KEY` | Chat LLM 密钥 | `replace_with_your_deepseek_api_key` |
| `LLM_CHAT_BASE_URL` | OpenAI Chat Completions 兼容根地址 | `https://api.deepseek.com` |
| `LLM_CHAT_MODEL` | Chat 模型名称 | `deepseek-flash` |

文档上传、查询向量生成、问题改写和回答生成都会调用相应真实 Provider，可能产生模型费用。用户问题与检索出的授权知识片段会发送到 `LLM_CHAT_BASE_URL` 指向的外部服务；客服和管理员的问答可能包含内部文档片段。部署者必须确认该传输符合实际数据安全和合规要求。自动化测试默认使用 Fake Provider 或本地 HTTP 服务，不调用公网模型；真实密钥只保存在被 Git 忽略的 `.env` 中。向量 Provider 与 Chat Provider 可以使用不同厂商和不同密钥。

### 修改端口

如果 8080 端口被占用，可以在启动前设置其他端口：

```powershell
$env:PORT = "8090"
go run ./cmd/server
```

对应访问地址变为 `http://127.0.0.1:8090`。

## 手机同网段访问

### 第一步：连接同一个网络

确认电脑和手机连接同一个 Wi-Fi。电脑使用网线时，也应确保网线和手机 Wi-Fi 属于同一个局域网。

### 第二步：启动服务

先按“启动项目”章节启动 PostgreSQL，再在电脑上执行：

```powershell
cd D:\ProjectForYingBai\backend
go run ./cmd/server
```

服务必须保持运行。首次启动时，如果 Windows 防火墙询问是否允许网络访问，请勾选“专用网络”并允许访问。

### 第三步：查找电脑局域网 IP

新开一个 PowerShell 窗口，执行：

```powershell
ipconfig
```

找到当前正在使用的“无线局域网适配器 WLAN”或“以太网适配器”下的 IPv4 地址，例如：

```text
IPv4 地址 . . . . . . . . . . . . : 192.168.1.20
```

不要使用 `127.0.0.1`，它只代表当前设备自身。

### 第四步：在手机打开

在手机浏览器或微信内置浏览器输入：

```text
http://192.168.1.20:8080
```

将 `192.168.1.20` 替换为电脑实际的 IPv4 地址。进入页面后：

1. 输入称呼；
2. 选择体验角色；
3. 点击“进入智能客服”；
4. 发送任意非空消息；
5. 对已入库知识提问，收到知识回答；点击“引用来源”可查看文件名。无依据问题会返回“知识库暂无依据，请转人工”。

也可以先访问下面的地址检查后端连接：

```text
http://192.168.1.20:8080/health
```

正常情况下会显示：

```json
{
  "status": "ok",
  "service": "customer-service-backend"
}
```

### 手机无法访问时

按以下顺序检查：

1. 服务窗口是否仍在运行；
2. 手机和电脑是否连接同一个 Wi-Fi；
3. 地址是否使用电脑的 IPv4，而不是 `127.0.0.1`；
4. 端口是否与启动时一致，默认是 `8080`；
5. Windows 防火墙是否允许 Go 在“专用网络”通信；
6. 临时关闭手机和电脑上的 VPN 或代理后重试；
7. 路由器是否开启了“访客隔离”或“AP 隔离”。

## 接口说明

### 健康检查

```http
GET /health
```

成功响应：

```json
{
  "status": "ok",
  "service": "customer-service-backend"
}
```

### 聊天接口

```http
POST /chat
Content-Type: application/json
```

请求示例：

```json
{
  "message": "样品包装破损时应该怎么处理？",
  "sessionId": "session-1",
  "role": "customer"
}
```

成功响应（成功返回前会先写入 SQLite 对话日志）：

```json
{
  "answer": "应暂停样品流转并记录异常。[S1]",
  "type": "knowledge",
  "sessionId": "session-1",
  "role": "customer",
  "citations": [
    {
      "sourceId": "S1",
      "documentId": "example-document-id",
      "originalName": "样品接收规范.md",
      "chunkIndex": 0,
      "similarity": 0.91
    }
  ]
}
```

无知识依据或所有结果低于相似度阈值时返回 HTTP 200：

```json
{
  "answer": "知识库暂无依据，请转人工",
  "type": "refusal",
  "sessionId": "session-1",
  "role": "customer",
  "citations": []
}
```

`message`、`sessionId` 为空或请求格式错误时返回 HTTP 400：

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "message 和 sessionId 不能为空，message 不能超过 2000 个字符"
  }
}
```

SQLite 日志写入失败时不会返回未持久化的答案，而是返回 HTTP 500 和稳定错误码 `CHAT_LOG_FAILED`。日志包含 `sessionId`、角色、问题、答案、回答类型、去重后的引用文件名和 UTC 创建时间，不保存密钥、完整 Prompt 或知识片段正文。

### 文档上传接口

```http
POST /api/v1/documents
Content-Type: multipart/form-data
```

支持 `.pdf`、`.txt`、`.md`，单个文件最大 20 MB。上传必须同时提供以下 multipart 表单字段：

| 字段 | 可选值 |
| --- | --- |
| `file` | PDF、TXT 或 MD 文件 |
| `category` | `纺织`、`鞋类`、`杂货` |
| `type` | `标准`、`业务规范`、`FAQ` |
| `permission` | `公开`、`内部` |

上传成功前会完成文本提取、语义边界切片、千问向量化和 pgvector 事务存储。切片优先遵循标题、段落和句末标点，默认目标长度为 700 个 Unicode 字符、最大 1000 个字符；仅在单个句子超过上限时进行安全兜底切分。Provider 每批最多发送 10 个分块，返回向量必须为 1024 维，全部完成后状态为 `vectorized`。

上传文档的提取文本会发送到 `.env` 中 `LLM_BASE_URL` 指向的外部模型服务，包括权限为“内部”的文档。部署者必须确保该传输符合实际数据安全和合规要求。

PowerShell 调用示例：

```powershell
curl.exe -X POST "http://127.0.0.1:8080/api/v1/documents" -F "file=@D:\path\sample.pdf" -F "category=纺织" -F "type=标准" -F "permission=公开"
```

成功时返回 HTTP 201：

```json
{
  "document": {
    "id": "服务端生成的文档标识",
    "originalName": "sample.pdf",
    "format": "pdf",
    "size": 1024,
    "category": "纺织",
    "type": "标准",
    "permission": "公开",
    "status": "vectorized",
    "extractionStatus": "completed",
    "textLength": 3560,
    "chunkCount": 6,
    "embeddingModel": "qwen3.7-text-embedding",
    "embeddingDimensions": 1024,
    "uploadedAt": "2026-09-24T09:00:00Z",
    "updatedAt": "2026-09-24T09:00:08Z"
  }
}
```

原始上传文件默认保存在 `data/uploads/`，并使用服务端生成的名称；文档元数据、提取文本分块和向量保存在 PostgreSQL/pgvector。成功状态、分块和向量在同一事务中写入。文件落盘后若提取、切片或向量化失败，系统会保留原文件和状态为 `failed` 的元数据，记录安全失败原因供管理页重试，但失败文档不会参与 RAG 检索。`data/uploads/` 不会提交到 Git，也不会生成 `.extracted.txt` 或 `.metadata.json`。

系统以原始文件内容的 SHA-256 去重，与文件名无关。重复内容返回 HTTP 409，并在错误体的 `documentId` 中给出已有文档标识。

可能的错误码：

- `FILE_REQUIRED`：没有文件或文件为空；
- `UNSUPPORTED_FILE_TYPE`：不是 PDF、TXT、MD；
- `FILE_TOO_LARGE`：文件超过 20 MB；
- `INVALID_MULTIPART`：上传请求格式错误；
- `DUPLICATE_DOCUMENT`：相同内容的文档已经存在；
- `DOCUMENT_PROCESSING_FAILED`：服务端处理或保存失败；
- `INVALID_CATEGORY`：品类不在允许范围内；
- `INVALID_DOCUMENT_TYPE`：文档类型不在允许范围内；
- `INVALID_PERMISSION`：权限不是公开或内部。
- `NO_EXTRACTABLE_TEXT`：文档为空或 PDF 中没有可提取文本；
- `TEXT_EXTRACTION_FAILED`：文本编码错误、PDF 损坏或解析失败；
- `EXTRACTED_TEXT_TOO_LARGE`：提取文本超过 10 MB。
- `TEXT_CHUNKING_FAILED`：提取文本无法完成切片。
- `EMBEDDING_FAILED`：外部向量服务失败、返回数量不符或向量维度不是 1024。

### 文档列表接口

```http
GET /api/v1/documents?page=1&pageSize=20&q=&category=&type=&permission=&status=
```

按上传时间倒序返回文档及其元数据。支持以下查询参数：

| 参数 | 说明 |
| --- | --- |
| `page` | 页码，默认 1 |
| `pageSize` | 每页数量，默认 20，允许 1～100 |
| `q` | 原始文件名模糊搜索 |
| `category` | `纺织`、`鞋类`、`杂货` |
| `type` | `标准`、`业务规范`、`FAQ` |
| `permission` | `公开`、`内部` |
| `status` | `vectorized`、`failed` |

非法分页或筛选返回 HTTP 400 和 `INVALID_DOCUMENT_FILTER`。成功响应：

```json
{
  "documents": [
    {
      "id": "服务端生成的文档标识",
      "originalName": "sample.pdf",
      "format": "pdf",
      "size": 1024,
      "category": "纺织",
      "type": "标准",
      "permission": "公开",
      "status": "vectorized",
      "extractionStatus": "completed",
      "textLength": 3560,
      "chunkCount": 6,
      "embeddingModel": "qwen3.7-text-embedding",
      "embeddingDimensions": 1024,
      "uploadedAt": "2026-09-24T09:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "pageSize": 20,
  "totalPages": 1
}
```

没有文档时返回：

```json
{
  "documents": [],
  "total": 0,
  "page": 1,
  "pageSize": 20,
  "totalPages": 0
}
```

### 删除、失败重试与重新向量化

```http
DELETE /api/v1/documents/:id
POST /api/v1/documents/:id/retry
POST /api/v1/documents/:id/revectorize
```

- 删除会移除服务端原始文件，并由 PostgreSQL 级联删除对应分块和向量。
- `retry` 仅用于状态为 `failed` 的文档，使用保留的原始文件重新执行完整处理链路。
- `revectorize` 会重新提取、切片和生成向量；成功后事务替换旧分块。处理失败时旧的可用分块仍保留，并在文档上记录最新失败原因。
- 文档不存在返回 `DOCUMENT_NOT_FOUND`；对非失败文档调用重试返回 `DOCUMENT_NOT_RETRYABLE`。

## 自动化验证

后端不需要提前启动。执行：

```powershell
cd D:\ProjectForYingBai\backend
go test ./...
```

普通测试使用内存存储替身，不会污染开发数据库。包含 PostgreSQL 事务测试的完整验收命令为：

```powershell
cd D:\ProjectForYingBai
docker compose up -d --wait postgres
cd backend
$env:TEST_DATABASE_URL = "postgres://yingbai:yingbai_dev_password@localhost:5432/yingbai?sslmode=disable"
go test ./...
go vet ./...
go build ./cmd/server
```

集成测试使用唯一文档标识并在结束后清理测试记录，验证元数据与分块写入、列表读取和事务回滚。

查看每条测试的详细结果：

```powershell
go test -v ./...
```

当前自动化测试覆盖：

- 健康检查成功；
- 聊天接口完整 RAG 流程、固定拒答与引用返回；
- 空检索或低相似度时不调用最终回答模型；
- 成功知识回答与固定拒答在 HTTP 200 前写入 SQLite；
- SQLite 迁移可重复执行，数据库关闭重开后日志仍可读取；
- 日志失败返回稳定的 `CHAT_LOG_FAILED`，固定拒答日志保持空引用；
- 缺少消息时拒绝请求；
- 移动端首页可访问；
- 文档管理页面可访问并包含上传、筛选与文档操作入口；
- 页面包含基础安全响应头；
- PDF、TXT、MD 上传、提取、语义切片并正确保存；
- 非法格式被拒绝且不会落盘；
- 缺少文件时返回稳定错误；
- 非法品类、类型和权限被拒绝且不会落盘；
- 文档元数据通过持久化存储在服务重建后仍可读取；
- 空文档列表返回稳定的空数组；
- TXT、MD 的 UTF-8 文本和 PDF 文本能够正确提取；
- 空文本、非法 UTF-8、损坏 PDF 和向量化失败会保存安全失败原因与可重试记录；
- SHA-256 内容去重及并发唯一约束；
- 文档列表分页、文件名和业务元数据组合筛选；
- 失败文档重试、已入库文档重新向量化、原始文件删除与数据库级联删除；
- 标题、段落和句末标点边界切片；
- PostgreSQL 中元数据和文本分块的事务写入与失败回滚。
- Fake Provider 对相同文本生成固定维度、可重复的归一化向量；
- OpenAI Compatible Provider 的 URL、鉴权、模型、批量输入、返回顺序和异常响应校验；
- Provider 配置完全从环境变量读取，测试不使用真实密钥或公网模型。
- 默认测试使用 1024 维 Fake Provider，并验证向量随分块写入 pgvector；
- 11 个输入会拆分为 10+1 两次兼容请求；
- 显式启用的真实千问测试验证返回 1024 维向量。
- Top-K 默认值、配置范围和非法配置失败行为；
- 查询文本通过 Provider 生成 1024 维向量；
- pgvector 余弦相似度排序与 Top-K 数量限制；
- `customer` 仅检索公开知识，`service` 和 `admin` 可检索公开及内部知识；
- 权限、文档状态和向量模型均在数据库排序及限制前完成过滤。
- Chat Provider 的 `/chat/completions` 路径、鉴权、模型、消息和生成参数契约；
- 问题改写只输出检索问题，并保留关键编号和限定条件的 Prompt 约束；
- 授权知识片段以带来源编号的 JSON 数据拼入 Prompt，并明确防止执行片段内指令；
- 无检索结果时不构造回答 Prompt；
- `TEST_LIVE_CHAT_LLM=1` 显式开启真实 Chat LLM 最小连通性测试。
- 相似度阈值以下的分块不会进入回答 Prompt；
- 移动端安全渲染答案正文，并通过可折叠区域展示去重后的引用文件名。

补齐本地 `.env` 中的三个 `LLM_CHAT_*` 变量后，可从 `backend/` 目录显式验证真实 Chat LLM；测试会自动加载项目根目录 `.env`，且只记录模型名和输出字符数：

```powershell
$env:TEST_LIVE_CHAT_LLM = "1"
go test -v ./internal/llm -run TestLiveOpenAICompatibleChatCompletion
Remove-Item Env:TEST_LIVE_CHAT_LLM
```

## 当前文档能力边界

- 保存原始文件，并在 PostgreSQL/pgvector 中保存元数据、语义文本分块和 1024 维向量；
- 提供同源文档管理页、内容去重、分页筛选、删除、失败重试与重新向量化；
- PDF 仅提取其文本层，不提供 OCR，纯图片扫描件会返回无可提取文本；
- 已完成切片、向量化、Top-K 检索、问题改写、Grounded Prompt、最终回答、固定拒答和引用返回；
- 已完成 SQLite 对话日志；默认文件为 `data/runtime/conversations.db`，不提供面向移动端的日志查询接口；
- 当前向量列固定为 1024 维，更换为其他维度的模型前必须同步执行数据库迁移；
- 当前没有提供直接返回文本分块的公网接口，分块由后续检索模块在后端内部使用。
