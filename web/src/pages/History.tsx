export default function History() {
  return (
    <div className="space-y-4">
      <h1 className="text-3xl font-semibold tracking-tight">历史记录</h1>
      <p className="text-muted-foreground">
        对话历史存档功能即将上线，届时可以在此查看最近的语音对话、收藏精彩片段并导出文字稿。
      </p>
      <div className="rounded-2xl border border-dashed border-border bg-card/50 p-10 text-center text-muted-foreground">
        <p>暂无数据，开始一次实时对话后我们会自动保存记录。</p>
      </div>
    </div>
  );
}
