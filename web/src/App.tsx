import { Outlet, Route, Routes } from "react-router-dom";

import { AppShell } from "./components/AppShell";
import Discover from "./pages/Discover";
import History from "./pages/History";
import RealtimeChat from "./pages/RealtimeChat";
import RoleDirectory from "./pages/RoleDirectory";
import Settings from "./pages/Settings";

function ShellLayout() {
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}

export default function App() {
  return (
    <Routes>
      <Route element={<ShellLayout />}>
        <Route path="/" element={<Discover />} />
        <Route path="/roles" element={<RoleDirectory />} />
        <Route path="/chat" element={<RealtimeChat />} />
        <Route path="/history" element={<History />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
    </Routes>
  );
}
