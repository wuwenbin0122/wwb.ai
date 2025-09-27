import { useMemo } from "react";

import { Button } from "../components/ui/button";
import { useRoleStore } from "../stores/useRoleStore";

export default function RealtimeChat() {
  const { selected } = useRoleStore((state) => state);

  const headline = useMemo(
    () => selected?.name ?? "选择一个角色开始语音对话",
    [selected]
  );

  return (
    <div className="grid gap-8 lg:grid-cols-[280px_1fr_320px]">
      <aside className="space-y-4 rounded-2xl border border-border bg-card p-5">
        <h2 className="text-lg font-semibold">当前角色</h2>
        {selected ? (
          <div className="space-y-2 text-sm text-muted-foreground">
            <p className="text-base font-medium text-foreground">{selected.name}</p>
            <p>{selected.bio}</p>
            <div>
              <p className="font-medium text-foreground">擅长技能</p>
              <ul className="list-disc space-y-1 pl-5">
                {selected.skills?.map((skill) => (
                  <li key={skill.name}>{skill.name}</li>
                ))}
              </ul>
            </div>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            尚未选择角色，可前往角色目录或发现页挑选。
          </p>
        )}
        <Button variant="outline" asChild>
          <a href="/roles">选择角色</a>
        </Button>
      </aside>

      <main className="flex flex-col gap-4 rounded-2xl border border-border bg-card p-6">
        <header>
          <h1 className="text-2xl font-semibold">{headline}</h1>
          <p className="text-sm text-muted-foreground">
            连接语音输入设备以体验实时语音识别与字幕展示。后端提供 `/api/audio/asr/stream`
            WebSocket 接口，可持续发送 PCM 数据并接收转写结果。
          </p>
        </header>
        <div className="flex-1 rounded-xl border border-dashed border-border bg-background/60 p-6 text-sm text-muted-foreground">
          <p>语音对话 UI 将在此处呈现，包括字幕流、录音状态和播放控制。</p>
        </div>
        <div className="flex items-center gap-3">
          <Button className="flex-1" disabled>
            连接麦克风（开发中）
          </Button>
          <Button variant="outline">上传音频</Button>
        </div>
      </main>

      <aside className="space-y-4 rounded-2xl border border-border bg-card p-5">
        <h2 className="text-lg font-semibold">音色与参数</h2>
        <div className="space-y-3 text-sm text-muted-foreground">
          <p>可在此配置七牛云 TTS 音色、语速与音量，实时更新语音回复效果。</p>
          <div className="rounded-lg border border-dashed border-border p-4">
            <p className="text-xs uppercase tracking-wide text-muted-foreground">示例参数</p>
            <ul className="mt-2 space-y-1 text-sm">
              <li>音色：{selected?.personality.voice ?? "qiniu_zh_female_tmjxxy"}</li>
              <li>语速：1.0x</li>
              <li>采样率：16 kHz</li>
            </ul>
          </div>
        </div>
      </aside>
    </div>
  );
}
