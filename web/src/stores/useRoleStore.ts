import { create } from "zustand";

import type { Role } from "../components/RoleCard";

type RoleState = {
  selected?: Role;
  setSelected: (role?: Role) => void;
};

export const useRoleStore = create<RoleState>((set) => ({
  selected: undefined,
  setSelected: (role) => set({ selected: role })
}));
