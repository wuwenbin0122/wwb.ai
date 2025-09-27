import { FormEvent, useEffect, useState } from "react";

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

export default function RoleDirectory() {
  const { selected, setSelected } = useRoleStore((state) => state);
  const [roles, setRoles] = useState<Role[]>([]);
  const [domain, setDomain] = useState("");
  const [search, setSearch] = useState("");
  const [tags, setTags] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();

  const fetchRoles = (params: { domain?: string; search?: string; tags?: string }) => {
    const query = new URLSearchParams({ page: "1", page_size: "12" });
    if (params.domain) query.append("domain", params.domain);
    if (params.search) query.append("search", params.search);
    if (params.tags) query.append("tags", params.tags);

    setLoading(true);
    setError(undefined);
    fetch(`/api/roles?${query.toString()}`)
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`request failed with status ${res.status}`);
        }
        const payload = (await res.json()) as RolesResponse;
        setRoles(payload.data ?? []);
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    fetchRoles({});
  }, []);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    fetchRoles({ domain, search, tags });
  };

  return (
    <div className="space-y-10">
      <header className="space-y-3">
        <h1 className="text-3xl font-semibold tracking-tight">角色目录</h1>
        <p className="text-muted-foreground">
          根据领域、标签或关键词搜索合适的角色，了解他们的背景与技能。
        </p>
      </header>

      <form
        onSubmit={handleSubmit}
        className="grid gap-4 rounded-2xl border border-border bg-card p-6 md:grid-cols-4"
      >
        <label className="flex flex-col gap-2 text-sm">
          <span className="text-muted-foreground">领域</span>
          <input
            type="text"
            value={domain}
            onChange={(event) => setDomain(event.target.value)}
            placeholder="如：Philosophy"
            className="h-10 rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </label>
        <label className="flex flex-col gap-2 text-sm">
          <span className="text-muted-foreground">关键词</span>
          <input
            type="text"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="角色或简介关键词"
            className="h-10 rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </label>
        <label className="flex flex-col gap-2 text-sm">
          <span className="text-muted-foreground">标签</span>
          <input
            type="text"
            value={tags}
            onChange={(event) => setTags(event.target.value)}
            placeholder="用逗号分隔，例如 brave,strategist"
            className="h-10 rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </label>
        <div className="flex items-end">
          <Button type="submit" className="w-full">
            搜索
          </Button>
        </div>
      </form>

      {loading && <p className="text-muted-foreground">正在加载角色...</p>}
      {error && !loading && <p className="text-destructive">加载失败：{error}</p>}

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
          {roles.length === 0 && (
            <p className="col-span-full text-center text-muted-foreground">暂无匹配的角色</p>
          )}
        </div>
      )}
    </div>
  );
}
