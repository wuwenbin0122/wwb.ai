# AI 角色扮演聊天机器人

本项目旨在创建一个基于 AI 的角色扮演聊天机器人，允许用户与各种历史、文学、影视等角色进行语音互动。通过语音识别（STT）和语音合成（TTS）技术，用户可以通过语音与角色进行流畅对话。项目使用 Go 语言开发后端，前端将由 AI 辅助完成。

___

## 技术栈

- **后端**：Go 语言，Gin 框架，WebSocket
- **语音识别（STT）**：七牛云语音识别 API
- **语音合成（TTS）**：七牛云语音合成 API
- **数据库**：PostgreSQL（关系型数据库），MongoDB（文档数据库），Redis（缓存）
- **容器化**：Docker 和 Kubernetes
- **监控与日志**：Prometheus、Grafana、Zap
___
## 功能

### 核心功能

1. **角色选择与搜索**：
   - 用户可以根据角色名称、领域（如文学、历史、影视等）、标签（如“幽默”“严肃”）进行搜索，推荐热门角色。
   
2. **语音实时聊天**：
   - 用户通过语音与角色对话，AI 会通过语音识别（STT）转化语音为文字，处理后通过语音合成（TTS）输出回应。

3. **角色人设还原**：
   - AI 根据角色的背景（例如：苏格拉底的反问，哈利波特的魔法术语）还原角色的语气、用词习惯和知识背景。

### 体验增强功能

1. **聊天记录保存与回溯**：
   - 用户可以查看或回放历史对话，收藏精彩对话片段。

2. **多语言支持**：
   - 角色能够在多种语言下进行互动，例如：莎士比亚可以用中文讨论戏剧。

3. **角色状态调整**：
   - 用户可以调整角色的“活跃度”“严肃度”，例如让“鲁迅”变得更加温和或更加犀利。
___
## 安装与运行

### 1. 克隆项目

```bash
git clone git@github.com:wuwenbin0122/wwb.ai.git
cd wwb.ai
```

### 2. 安装依赖

确保您已安装 Go 1.18 及以上版本，运行以下命令安装 Go 依赖：

```bash
go mod tidy
```

### 3. 配置环境变量

确保您配置了七牛云的 API 密钥、数据库连接等环境变量。可以在 `.env` 文件中设置环境变量：

```bash
DB_USER=your_user
DB_PASSWORD=your_password
DB_NAME=your_db
QINIU_ACCESS_KEY=your_access_key
QINIU_SECRET_KEY=your_secret_key
```

### 4. 启动后端服务

运行以下命令启动 Go 后端服务：

```bash
go run cmd/server/main.go
```

后端服务将在 `http://localhost:8080` 上运行。

### 5. 启动前端服务

前端由 AI 辅助完成，您可以通过以下命令启动前端开发环境：

```bash
cd web
npm install
npm start
```

前端将会在 `http://localhost:3000` 上运行。
___
## 许可证

本项目遵循 Apache-2.0  许可证，详细信息请查看 [LICENSE](./LICENSE) 文件。
