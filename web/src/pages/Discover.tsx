import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import { RoleCard, type Role } from "../components/RoleCard";
import { Button } from "../components/ui/button";
import { useRoleStore } from "../stores/useRoleStore";

type RolesResponse = {
  data: Role[];
  pagination: {
    page: number;
    page_size: number;
    total: number;
  };
};

export default function Discover() {
  const setSelected = useRoleStore((state) => state.setSelected);
  const selected = useRoleStore((state) => state.selected);
  const [roles, setRoles] = useState<Role[]>([]);
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let mounted = true;
    setLoading(true);
    fetch("/api/roles?page=1&page_size=6")
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`request failed with status ${res.status}`);
        }
        const payload = (await res.json()) as RolesResponse;
        if (mounted) {
          setRoles(payload.data ?? []);
        }
      })
      .catch((err: Error) => {
        if (mounted) {
          setError(err.message);
        }
      })
      .finally(() => {
        if (mounted) {
          setLoading(false);
        }
      });

    return () => {
      mounted = false;
    };
  }, []);

  const heroRole = useMemo(() => roles[0], [roles]);

  return (
    <div className="space-y-12">
      <section className="rounded-3xl bg-gradient-to-r from-primary/10 via-primary/5 to-primary/10 p-10">
        <div className="flex flex-col gap-6 lg:flex-row lg:items-center lg:justify-between">
          <div className="space-y-4">
            <p className="text-sm uppercase tracking-[0.4em] text-primary">
              AI Roleplay Assistant
            </p>
            <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl lg:text-5xl">
              与风格各异的 AI 角色开启沉浸式语音对话
            </h1>
            <p className="max-w-2xl text-base text-muted-foreground">
              选择你喜欢的角色，即刻体验实时语音识别与自然语言生成带来的沉浸式互动。
              借助角色的背景与技能设定，获取灵感、陪伴或专业建议。
            </p>
            <div className="flex flex-wrap gap-3">
              <Button asChild>
                <Link to="/chat">开始实时对话</Link>
              </Button>
              <Button asChild variant="outline">
                <Link to="/roles">浏览角色目录</Link>
              </Button>
            </div>
          </div>
          {heroRole && (
            <div className="w-full max-w-md self-stretch rounded-2xl bg-white/70 p-6 shadow-lg backdrop-blur lg:max-w-sm">
              <p className="text-sm font-medium text-muted-foreground">今日推荐角色</p>
              <h2 className="mt-2 text-2xl font-semibold text-foreground">{heroRole.name}</h2>
              <p className="mt-2 text-sm text-muted-foreground">{heroRole.bio}</p>
              <div className="mt-4 text-xs text-muted-foreground">
                <p>擅长语言：{heroRole.languages.join(" / ")}</p>
                <p>背景：{heroRole.background}</p>
              </div>
            </div>
          )}
        </div>
      </section>

      <section className="space-y-6">
        <div className="flex items-center justify-between">
          <h2 className="text-2xl font-semibold">热门角色</h2>
          <Button asChild variant="ghost" className="text-sm">
            <Link to="/roles">查看更多</Link>
          </Button>
        </div>
        {loading && <p className="text-muted-foreground">正在加载角色...</p>}
        {error && !loading && (
          <p className="text-destructive">加载角色失败：{error}</p>
        )}
        {!loading && !error && (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {roles.map((role) => (
              <RoleCard
                key={role.id}
                role={role}
                onSelect={setSelected}
                active={selected?.id === role.id}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
