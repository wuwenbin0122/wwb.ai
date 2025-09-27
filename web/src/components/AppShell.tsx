import { PropsWithChildren } from "react";
import { Link, useLocation } from "react-router-dom";

import { cn } from "../lib/utils";

const navItems = [
  { to: "/", label: "发现" },
  { to: "/roles", label: "角色目录" },
  { to: "/chat", label: "实时对话" },
  { to: "/history", label: "历史" },
  { to: "/settings", label: "设置" }
];

export function AppShell({ children }: PropsWithChildren) {
  const location = useLocation();

  return (
    <div className="min-h-screen bg-background">
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-8 px-6 py-10">
        <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">WWB.AI 语音助手</h1>
            <p className="text-sm text-muted-foreground">
              基于七牛云 ASR/TTS 的实时语音对话体验
            </p>
          </div>
          <nav className="flex flex-wrap gap-2">
            {navItems.map((item) => {
              const active = location.pathname === item.to;
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  className={cn(
                    "rounded-full px-4 py-2 text-sm font-medium transition",
                    active
                      ? "bg-primary text-primary-foreground shadow"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </header>
        <main className="pb-20">{children}</main>
      </div>
    </div>
  );
}
