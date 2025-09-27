import { memo } from "react";

import { Badge } from "./ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { cn } from "../lib/utils";

export type Skill = {
  name: string;
  description: string;
};

export type Role = {
  id: number;
  name: string;
  domain: string;
  tags: string[];
  bio: string;
  personality: Record<string, string>;
  background: string;
  languages: string[];
  skills: Skill[];
};

type RoleCardProps = {
  role: Role;
  onSelect?: (role: Role) => void;
  active?: boolean;
};

export const RoleCard = memo(({ role, onSelect, active }: RoleCardProps) => {
  return (
    <button
      type="button"
      className={cn("text-left", active && "ring-2 ring-primary ring-offset-2")}
      onClick={() => onSelect?.(role)}
    >
      <Card className="h-full w-full transition hover:shadow-md">
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            <span>{role.name}</span>
            <span className="text-sm font-normal text-muted-foreground">{role.domain}</span>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-sm text-muted-foreground">
          <p>{role.bio}</p>
          <div className="flex flex-wrap gap-2">
            {role.tags?.map((tag) => (
              <Badge key={tag} variant="outline">
                #{tag}
              </Badge>
            ))}
          </div>
        </CardContent>
      </Card>
    </button>
  );
});

RoleCard.displayName = "RoleCard";
