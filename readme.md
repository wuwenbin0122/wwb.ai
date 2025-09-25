# AI 角色扮演语音助手

基于七牛云语音识别（ASR）与语音合成（TTS）能力构建的实时语音对话体验。用户可以在 Web 端选择预设角色，进行语音输入、字幕查看以及语音播放，整套流程已经按照《AI 角色扮演前端 UI／交互方案 V1》进行了界面与交互重构。

---

## 亮点能力

- **REST 化的语音能力接入**：已替换旧版 WebSocket 流程，统一通过 `https://openai.qiniu.com/v1`（或备用域名）调用七牛云 `/voice/asr`、`/voice/tts`、`/voice/list` 等接口。
- **三栏实时对话工作台**：左侧角色切换，中部显示字幕与回复气泡，右侧提供音色选择、语速滑块与音频播放器，契合产品设计稿。
- **音色库与语速配置**：前端可拉取音色列表、试听音色，并在发送 TTS 请求时动态选择音色与语速。
- **角色目录与发现页**：重构角色目录筛选、热门角色展示及搜索框，支持从“发现/角色目录”一键跳转到实时对话。
- **可扩展的应用骨架**：保留 MongoDB / PostgreSQL / Redis 依赖注入，方便后续接入会话存档、角色管理等高级能力。

---

## 技术栈

| 层级 | 说明 |
| --- | --- |
| 后端 | Go 1.25、Gin、Zap 日志、官方 `net/http` 调用七牛 REST API |
| 前端 | React + Vite、原子化 CSS（自定义样式表）、Web Audio / AudioWorklet 录音处理 |
| 数据层 | PostgreSQL、MongoDB、Redis（当前主要用于未来扩展） |

---

## 配置项

在 `config/.env` 或环境变量中设置以下键值：

```dotenv
# 数据库与缓存（如未使用可指向测试实例）
DB_URL=postgres://user:pass@localhost:5432/postgres
MONGO_URI=mongodb://localhost:27017/local
REDIS_URL=localhost:6379

# 七牛云语音能力
QINIU_API_KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
QINIU_API_BASE_URL=https://openai.qiniu.com/v1   # 可切换为 https://api.qnaigc.com/v1
QINIU_TTS_VOICE_TYPE=qiniu_zh_female_tmjxxy      # 默认音色
QINIU_TTS_FORMAT=mp3                             # 默认音频编码，可选 ogg等
QINIU_ASR_MODEL=asr                              # 当前官方模型名
QINIU_NLP_MODEL=doubao-1.5-vision-pro            # 文本生成模型

# 服务监听地址
SERVER_ADDR=:8080
```

> 若环境变量缺失，程序会尝试读取 `config/.env` 文件；数据库配置仍为必填，以便保持与既有业务兼容。

---

## 启动指南

### 1. 初始化依赖

```bash
go mod tidy
cd web
npm install
```

### 2. 启动后端

```bash
go run cmd/server/main.go
```

默认监听 `http://localhost:8080`，提供以下接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET`  | `/api/roles`          | 角色目录查询（支持 `domain`、`tags`） |
| `POST` | `/api/nlp/chat`        | 组合系统提示并转发至七牛大模型，返回助手回复 |
| `POST` | `/api/audio/asr`      | 提交音频（Base64 或 URL）并获取转写文本 |
| `POST` | `/api/audio/tts`      | 文本合成语音，返回 Base64 音频串 |
| `GET`  | `/api/audio/voices`   | 拉取七牛官方音色列表 |
| `GET`  | `/health`             | 健康检查 |

### 3. 启动前端

```bash
cd web
npm run dev
```

开发环境默认监听 `http://localhost:5173`。构建产物可通过 `npm run build` 生成。

---

## 前端交互速览

- **发现页**：渐变背景的 Hero 区、热门角色九宫格、快捷跳转按钮。
- **角色目录**：左侧筛选域、右侧响应式卡片列表，展示筛选 Chip、结果总数。
- **实时对话**：三列布局 —— 角色列表、字幕聊天流、音色/语速配置，并展示录音状态、错误信息和音频播放器。
- **音色库**：右侧面板支持刷新音色列表，提供试听链接与快速选择。
- **设置 / 历史**：按设计稿提供占位模块，便于后续接入账号、设备测试与对话回放。

---

## 接口示例

### 语音识别（后端会转发至七牛）

```bash
curl -X POST http://localhost:8080/api/audio/asr \
  -H "Content-Type: application/json" \
  -d '{
        "audio_format": "wav",
        "audio_data": "<Base64>"
      }'
```

响应体将包含：

```json
{
  "reqid": "...",
  "text": "识别出的文本",
  "duration_ms": 1673,
  "raw": { "reqid": "...", "data": { ... } }
}
```

### 语音合成

```bash
curl -X POST http://localhost:8080/api/audio/tts \
  -H "Content-Type: application/json" \
  -d '{
        "text": "你好，世界！",
        "voice_type": "qiniu_zh_female_tmjxxy",
        "encoding": "mp3",
        "speed_ratio": 1.0
      }'
```

成功时会返回 Base64 编码的音频数据，可直接在浏览器或前端转为可播放的 Blob。

---

## 后续规划

- 会话记录与收藏：结合 Redis/MongoDB 实现历史对话时间线及片段收藏。
- 角色管理后台：为运营/创作者提供 Prompt 片段、技能配置等管理界面。
- 多模态能力：结合七牛图片 / 视频 API 打造更丰富的互动形式。

欢迎提交 Issue 或 Pull Request 与我们共同完善体验。
