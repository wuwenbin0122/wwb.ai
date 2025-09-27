# WWB.AI 实时语音助手

基于七牛云 ASR/TTS 能力构建的语音角色扮演体验。项目提供可运行的 Go 后端与 Vite + React + TypeScript 前端骨架，内置角色数据、环境配置和基础界面，方便后续扩展实时对话、对话历史等业务能力。

---

## 技术栈

| 层级 | 说明 |
| --- | --- |
| 后端 | Go 1.25 · Gin · **GORM** · Zap · `net/http` 调用七牛 REST/WebSocket ASR 服务 |
| 前端 | Vite · React 18 · TypeScript · Tailwind CSS · shadcn/ui 组件 · Zustand 状态管理 |
| 数据 | PostgreSQL（角色表） · Redis · MongoDB（预留扩展能力） |

---

## 环境变量

后端会首先读取系统环境变量，若缺失则回退到 `config/.env`。关键配置如下：

| 变量名 | 说明 | 示例 |
| --- | --- | --- |
| `DB_URL` | Postgres 连接串 | `postgres://user:pass@localhost:5432/postgres` |
| `MONGO_URI` | MongoDB 连接串 | `mongodb://localhost:27017/local` |
| `REDIS_URL` | Redis 地址 | `localhost:6379` |
| `QINIU_API_BASE` | 七牛语音 API 域名 | `https://openai.qiniu.com/v1` |
| `QINIU_API_KEY` | 七牛鉴权 Token | `sk-xxxx` |
| `QINIU_TTS_VOICE_TYPE` | 默认 TTS 音色 | `qiniu_zh_female_tmjxxy` |
| `QINIU_TTS_FORMAT` | TTS 音频编码 | `mp3` |
| `QINIU_ASR_MODEL` | ASR 模型名 | `asr` |
| `ASR_SAMPLE_RATE` | 实时识别采样率（Hz） | `16000` |
| `SERVER_ADDR` | Go 服务监听地址 | `:8080` |

> `.env` 中已提供示例值，可根据环境调整。

---

## 初始化与运行

### 1. 拉取依赖

```bash
go mod tidy
cd web
npm install
```

### 2. 初始化数据库

```bash
# 重新创建 roles 表（包含 personality/background/languages/skills）
go run cmd/scripts/reset_roles_table/main.go

# 写入 4 个示例角色（苏格拉底、哈利波特、花木兰、夏洛克）
go run cmd/scripts/seed_roles/main.go
```

### 3. 启动后端

```bash
go run cmd/server/main.go
```

默认监听 `http://localhost:8080`，核心接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 健康检查 |
| `GET` | `/api/roles` | 角色列表，支持 `page` / `page_size` / `search` / `domain` / `tags` 查询 |
| `POST` | `/api/audio/asr` | 提交音频（URL 或 Base64）并转写 |
| `GET` | `/api/audio/asr/stream` | WebSocket，转发实时音频流 |
| `POST` | `/api/audio/tts` | 文本转语音 |
| `GET` | `/api/audio/voices` | 拉取七牛音色列表 |

### 4. 启动前端

```bash
cd web
npm run dev
```

开发模式运行在 `http://localhost:5173`，提供：

- **发现页**：Hero + 热门角色卡片，调用 `/api/roles` 渲染卡片。
- **角色目录**：领域 / 标签 / 搜索筛选，分页获取角色。
- **实时对话**：三栏布局，占位描述 WebSocket 流式识别流程。
- **历史 / 设置**：保留扩展位，提示后续可配置的内容。

构建生产包：`npm run build`。

---

## Docker Compose（可选）

项目包含 `docker-compose.yml`，启动 Postgres、Redis、Go 后端与 Vite 前端：

```bash
docker compose up --build
```

默认暴露：

- Backend: `http://localhost:8080`
- Frontend: `http://localhost:5173`
- Postgres: `localhost:5433`
- Redis: `localhost:6380`

---

## 常用脚本

| 命令 | 说明 |
| --- | --- |
| `go test ./...` | 后端单元测试 |
| `go run cmd/scripts/reset_roles_table/main.go` | 重建角色表 |
| `go run cmd/scripts/seed_roles/main.go` | 写入种子角色 |
| `npm run dev` | 启动前端开发服务器 |

---

## 后续扩展建议

- 结合 Redis/MongoDB 存储实时字幕、对话历史与收藏。
- 在设置页接入表单，支持动态调整采样率、音色等参数。
- 在实时对话页补齐麦克风录音、PCM 流推送与字幕渲染。

欢迎提交 Issue 或 PR 与我们共同完善体验。

