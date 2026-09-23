# 智能客服底座 MVP

面向纺织品、鞋类和消费品检测实验室的移动端智能客服底座。本阶段已完成 Day 1：移动端 H5、角色选择、聊天交互以及 Gin 后端基础接口联通。

## 当前能力

- 适配 375～430 像素宽度的移动端 H5；
- 外部客户、客服人员、管理员三种演示身份；
- 完整聊天界面、消息气泡、时间、发送中状态和失败重试；
- `GET /health` 健康检查；
- `POST /chat` 聊天接口，当前固定回复“底座已连通”；
- Gin 同源托管移动端页面，无需单独安装或启动前端工具；
- 同一局域网内可通过手机浏览器或微信内置浏览器访问。

当前角色入口是演示级身份选择，不是生产级登录认证。知识库、RAG、对话日志和 LIMS Mock 将在后续阶段接入。

## 环境要求

- Windows 10/11；
- Go 1.27 或兼容版本；
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
│  ├─ internal/handler/    # HTTP 接口
│  ├─ internal/router/     # 路由与测试
│  ├─ go.mod
│  └─ go.sum
├─ data/knowledge/         # 后续知识文档目录
├─ docs/CONSTRAINTS.md     # 工程约束文件
├─ target.md               # 本周任务书
└─ request.md              # 整体解决方案
```

## 启动项目

打开 PowerShell，执行：

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

在电脑上执行：

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

## 自动化验证

后端不需要提前启动。执行：

```powershell
cd D:\ProjectForYingBai\backend
go test ./...
```

查看每条测试的详细结果：

```powershell
go test -v ./...
```

当前自动化测试覆盖：

- 健康检查成功；
- 聊天接口固定回复；
- 缺少消息时拒绝请求；
- 移动端首页可访问；
- 页面包含基础安全响应头。



