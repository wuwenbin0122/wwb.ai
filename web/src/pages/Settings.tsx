export default function Settings() {
  return (
    <div className="space-y-4">
      <h1 className="text-3xl font-semibold tracking-tight">设置</h1>
      <p className="text-muted-foreground">
        可在此配置默认音色、语音识别模型、API Key 等全局参数。当前版本提供环境变量配置与占位界面，方便后续扩展。
      </p>
      <div className="space-y-6 rounded-2xl border border-border bg-card p-6">
        <div>
          <p className="text-sm font-medium text-foreground">七牛云 API Key</p>
          <p className="text-sm text-muted-foreground">
            后端通过 `QINIU_API_KEY` 环境变量读取，部署时请确保安全存储。
          </p>
        </div>
        <div>
          <p className="text-sm font-medium text-foreground">采样率</p>
          <p className="text-sm text-muted-foreground">
            当前默认采样率为 16kHz，可在 `.env` 中通过 `ASR_SAMPLE_RATE` 调整。
          </p>
        </div>
      </div>
    </div>
  );
}
