# AI 角色扮演语音助手

基于七牛云语音识别（ASR）与语音合成（TTS）能力构建的实时语音对话体验。用户可以在 Web 端选择预设角色，进行语音输入、字幕查看以及语音播放。

---

## 亮点能力

- **实时语音能力接入**： WebSocket 流程，通过 `https://openai.qiniu.com/v1`（或备用域名）调用七牛云 `/voice/asr`、`/voice/tts`、`/voice/list` 等接口。
- **三栏实时对话工作台**：左侧角色切换，中部显示字幕与回复气泡，右侧提供音色选择、语速滑块与音频播放器，契合产品设计稿。
- **音色库与语速配置**：前端可拉取音色列表、试听音色，并在发送 TTS 请求时动态选择音色与语速。
- **角色目录与发现页**：重构角色目录筛选、热门角色展示及搜索框，支持从“发现/角色目录”一键跳转到实时对话。
- **可扩展的应用骨架**：保留 MongoDB / PostgreSQL / Redis 依赖注入，方便后续接入会话存档、角色管理等高级能力。

---

## 技术栈

| 层级 | 说明 |
| --- | --- |
| 后端 | Go 1.25、Gin、Zap 日志、 调用七牛  API |
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

### 2.1 迁移数据库（roles 表扩展）

启用多语言/技能/人设字段前，请执行迁移（幂等）：

```bash
# 方式 A：使用内置脚本（推荐）
go run cmd/scripts/migrate_roles_table/main.go

# 可选：查看列结构
go run cmd/scripts/inspect_roles/main.go

# 方式 B：直接执行 SQL（等价）
# 参见 db/migrations/0002_expand_roles_table.up.sql
```

### 2.2 写入示例人设/技能（可选）

执行扩展版种子脚本，覆盖/补充部分示例角色的人设、技能、语言与背景：

```bash
go run cmd/scripts/seed_roles_extended/main.go
```

包含角色：Socrates、Sherlock Holmes、Mulan、Harry Potter。若已存在同名记录，会先删除再重建。

### 2.3 为更多角色自动补全技能（可选）

根据角色名称/领域/标签/简介的关键词自动推断技能，并在不覆盖已有自定义技能的前提下合并写回：

```bash
go run cmd/scripts/enrich_roles_skills/main.go
```

规则示例：
- 哲学/老师/教练/导师 → `socratic_questions`
- 历史/学者/科研/侦探 → `citation_mode`
- 心理/咨询/支持/勇敢/温暖 → `emo_stabilizer`
- 名称命中（如 Socrates/Plato/Confucius、Sherlock Holmes、Mulan/Harry）附加相应技能
```

默认监听 `http://localhost:8080`，提供以下接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET`  | `/api/roles`          | 角色目录查询（支持 `domain`、`tags`） |
| `POST` | `/api/nlp/chat`        | 组合系统提示并转发至七牛大模型，返回助手回复 |
| `GET`  | `/ws/audio/asr` (WS)  | WebSocket 代理：浏览器推送 PCM，后端代连七牛 |
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

### 语音识别（WebSocket 流式代理）

浏览器或命令行需改用 WebSocket 通道，并在查询参数中携带七牛颁发的 `token`：

```bash
wscat -c "ws://localhost:8080/ws/audio/asr?token=<QiniuToken>"
```

连接建立后：

1. 发送配置帧：`{"type":"start","sampleRate":16000,"channels":1,"bits":16}`。
2. 连续发送二进制 PCM（16bit/单声道/16kHz）分片。
3. 发送 `{"type":"stop"}` 结束流式识别。

服务端会转发至七牛 ASR，并推送 `transcript` 事件（含 `text` 与是否最终结果 `is_final`）。




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
