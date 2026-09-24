# 智能客服底座 MVP

面向纺织品、鞋类和消费品检测实验室的移动端智能客服底座。Day 1 已完成移动端与 Gin 后端联通；Day 2 已完成知识文档上传、文本提取、语义切片、千问向量化与 pgvector 持久化。

## 当前能力

- 适配 375～430 像素宽度的移动端 H5；
- 外部客户、客服人员、管理员三种演示身份；
- 完整聊天界面、消息气泡、时间、发送中状态和失败重试；
- `GET /health` 健康检查；
- `POST /chat` 聊天接口，当前固定回复“底座已连通”；
- `POST /api/v1/documents` 文档上传接口，支持 PDF、TXT、MD 和业务元数据；
- `GET /api/v1/documents` 文档列表接口；
- PDF、TXT、MD 文本提取与提取状态记录；
- 按标题、段落和句末标点进行语义边界切片；
- 文档元数据、文本分块和 1024 维向量通过 PostgreSQL/pgvector 事务保存；
- 提供 `LLMProvider` 抽象、OpenAI Compatible 向量实现和确定性 Fake 实现；
- Gin 同源托管移动端页面，无需单独安装或启动前端工具；
- 同一局域网内可通过手机浏览器或微信内置浏览器访问。

当前角色入口是演示级身份选择，不是生产级登录认证。相似度检索、RAG、对话日志和 LIMS Mock 将在后续阶段接入。

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
│  │  └─ styles.css
│  └─ index.html
├─ backend/                # Gin 后端
│  ├─ cmd/server/main.go   # 服务启动入口
│  ├─ internal/chunker/    # 语义边界切片
│  ├─ internal/config/     # .env 与环境变量配置
│  ├─ internal/database/   # PostgreSQL 连接与迁移
│  ├─ internal/handler/    # HTTP 接口
│  ├─ internal/llm/        # OpenAI Compatible 与 Fake Provider
│  ├─ internal/repository/ # PostgreSQL 数据访问
│  ├─ internal/router/     # 路由与测试
│  ├─ go.mod
│  └─ go.sum
├─ data/knowledge/         # 后续知识文档目录
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

数据库相关变量包括 `DATABASE_URL`、`POSTGRES_DB`、`POSTGRES_USER`、`POSTGRES_PASSWORD` 和 `POSTGRES_PORT`。示例密码只用于本地开发；共享或生产环境必须替换。后端启动时会自动执行 `backend/internal/database/migrations/` 中的 SQL 迁移。

向量 Provider 使用以下变量：

| 变量 | 说明 | 示例 |
| --- | --- | --- |
| `LLM_API_KEY` | 千问/百炼服务密钥 | `replace_with_your_dashscope_api_key` |
| `LLM_BASE_URL` | OpenAI 兼容 API 根地址，代码会追加 `/embeddings` | `https://your-workspace-id.cn-beijing.maas.aliyuncs.com/compatible-mode/v1` |
| `LLM_EMBEDDING_MODEL` | 千问向量模型名称 | `qwen3.7-text-embedding` |

目前 `.env` 中是占位密钥，真实调用前必须替换。上传流程尚未调用真实 Provider，所以开发和自动化测试不会产生模型费用。

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
5. 收到“底座已连通”。

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
  "message": "你好",
  "sessionId": "session-1",
  "role": "customer"
}
```

成功响应：

```json
{
  "answer": "底座已连通",
  "type": "fixed",
  "sessionId": "session-1",
  "role": "customer",
  "citations": []
}
```

`message` 为空或请求格式错误时返回 HTTP 400：

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "message 不能为空"
  }
}
```

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
    "uploadedAt": "2026-09-24T09:00:00Z"
  }
}
```

原始上传文件默认保存在 `data/uploads/`，并使用服务端生成的名称；文档元数据、提取文本分块和向量保存在 PostgreSQL/pgvector。三者在同一数据库事务中写入，任一分块或向量失败都会整体回滚并清理原始上传副本。`data/uploads/` 不会提交到 Git，不再生成 `.extracted.txt` 或 `.metadata.json`。

可能的错误码：

- `FILE_REQUIRED`：没有文件或文件为空；
- `UNSUPPORTED_FILE_TYPE`：不是 PDF、TXT、MD；
- `FILE_TOO_LARGE`：文件超过 20 MB；
- `INVALID_MULTIPART`：上传请求格式错误；
- `UPLOAD_FAILED`：服务端保存失败。
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
GET /api/v1/documents
```

按上传时间倒序返回已经保存的文档及其元数据：

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
  "total": 1
}
```

没有文档时返回：

```json
{
  "documents": [],
  "total": 0
}
```

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
- 聊天接口固定回复；
- 缺少消息时拒绝请求；
- 移动端首页可访问；
- 页面包含基础安全响应头；
- PDF、TXT、MD 上传、提取、语义切片并正确保存；
- 非法格式被拒绝且不会落盘；
- 缺少文件时返回稳定错误；
- 非法品类、类型和权限被拒绝且不会落盘；
- 文档元数据通过持久化存储在服务重建后仍可读取；
- 空文档列表返回稳定的空数组；
- TXT、MD 的 UTF-8 文本和 PDF 文本能够正确提取；
- 空文本、非法 UTF-8 和损坏 PDF 被拒绝且不留下半成品。
- 标题、段落和句末标点边界切片；
- PostgreSQL 中元数据和文本分块的事务写入与失败回滚。
- Fake Provider 对相同文本生成固定维度、可重复的归一化向量；
- OpenAI Compatible Provider 的 URL、鉴权、模型、批量输入、返回顺序和异常响应校验；
- Provider 配置完全从环境变量读取，测试不使用真实密钥或公网模型。
- 默认测试使用 1024 维 Fake Provider，并验证向量随分块写入 pgvector；
- 11 个输入会拆分为 10+1 两次兼容请求；
- 显式启用的真实千问测试验证返回 1024 维向量。

## 当前文档能力边界

- 保存原始文件，并在 PostgreSQL/pgvector 中保存元数据、语义文本分块和 1024 维向量；
- PDF 仅提取其文本层，不提供 OCR，纯图片扫描件会返回无可提取文本；
- 已完成切片和向量化，但尚未实现相似度检索与 RAG 问答；
- 当前向量列固定为 1024 维，更换为其他维度的模型前必须同步执行数据库迁移；
- 当前没有提供直接返回文本分块的公网接口，分块由后续检索模块在后端内部使用。
