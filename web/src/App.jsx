import React, { useEffect, useMemo, useState } from "react";
import RolesCatalog from "./pages/RoleSelectPage.jsx";
import VoiceChatPage from "./pages/VoiceChatPage.jsx";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";

const TABS = [
    { id: "home", label: "发现" },
    { id: "roles", label: "角色目录" },
    { id: "chat", label: "实时对话" },
    { id: "history", label: "历史与收藏" },
    { id: "settings", label: "设置" },
];

const buildQueryString = (filters) => {
    const params = new URLSearchParams();
    if (filters.domain.trim() !== "") {
        params.append("domain", filters.domain.trim());
    }
    if (filters.tags.trim() !== "") {
        params.append("tags", filters.tags.trim());
    }
    const query = params.toString();
    return query ? `?${query}` : "";
};

const ConnectionIndicator = ({ connected }) => (
    <div className={`connection-indicator ${connected ? "connected" : "disconnected"}`}>
        <span className="dot" />
        {connected ? "已连接" : "未连接"}
    </div>
);

const HomeTab = ({ roles, loading, error, onExplore, onStart }) => (
    <div className="home-tab">
        <section className="hero">
            <div>
                <h1>AI 角色扮演助手</h1>
                <p>与世界名人、虚构英雄或你的私人人设进行沉浸式实时语音对话。</p>
                <div className="hero-actions">
                    <button type="button" className="primary" onClick={onStart}>
                        立即开聊
                    </button>
                    <button type="button" className="ghost" onClick={onExplore}>
                        浏览角色目录
                    </button>
                </div>
            </div>
            <div className="hero-glow" aria-hidden="true" />
        </section>

        <section className="section">
            <div className="section-header">
                <h2>热门角色</h2>
                <button type="button" className="link" onClick={onExplore}>
                    查看全部角色 →
                </button>
            </div>
            {loading && <p className="muted">正在载入角色…</p>}
            {error && <p className="error">加载失败：{error}</p>}
            {!loading && !error && roles.length === 0 && <p className="muted">暂无角色，尝试调整筛选条件。</p>}
            <div className="role-grid">
                {roles.map((role) => (
                    <div key={role.id} className="role-card" role="button" tabIndex={0} onClick={() => onStart(role.id)}>
                        <div className="role-avatar" aria-hidden="true">
                            {role.name.slice(0, 2)}
                        </div>
                        <div>
                            <h3>{role.name}</h3>
                            <p>{role.bio || "暂无简介"}</p>
                            <div className="tags">
                                {(role.tags || "")
                                    .split(",")
                                    .map((tag) => tag.trim())
                                    .filter(Boolean)
                                    .slice(0, 3)
                                    .map((tag) => (
                                        <span key={tag} className="tag">
                                            {tag}
                                        </span>
                                    ))}
                            </div>
                        </div>
                    </div>
                ))}
            </div>
        </section>
    </div>
);

const HistoryTab = () => (
    <div className="history-tab">
        <h2>历史与收藏</h2>
        <p className="muted">这里将展示对话时间线、收藏片段与导出功能。下一阶段将接入后端会话存档 API。</p>
        <div className="history-placeholder">
            <div className="timeline-card" />
            <div className="timeline-card" />
            <div className="timeline-card" />
        </div>
    </div>
);

const SettingsTab = () => (
    <div className="settings-tab">
        <h2>设置</h2>
        <div className="settings-grid">
            <div className="settings-card">
                <h3>API Key</h3>
                <p className="muted">使用配置文件中的默认密钥，或在前端提供输入表单以覆盖。</p>
                <code>sk-************</code>
            </div>
            <div className="settings-card">
                <h3>音频设备</h3>
                <p className="muted">后续可以在此列出可用麦克风与扬声器，并提供测试按钮。</p>
            </div>
            <div className="settings-card">
                <h3>多语言与辅助功能</h3>
                <p className="muted">支持界面语言切换、字幕字号和高对比度模式等个性化设置。</p>
            </div>
        </div>
    </div>
);

const getInitialConnection = () => {
    if (typeof navigator === "undefined") {
        return true;
    }
    return navigator.onLine;
};

const App = () => {
    const [activeTab, setActiveTab] = useState("home");
    const [filters, setFilters] = useState({ domain: "", tags: "" });
    const [rolesState, setRolesState] = useState({ data: [], loading: false, error: null });
    const [selectedRoleId, setSelectedRoleId] = useState(null);
    const [voicesState, setVoicesState] = useState({ data: [], loading: false, error: null });
    const [isConnected, setIsConnected] = useState(getInitialConnection());

    useEffect(() => {
        const handleOnline = () => setIsConnected(true);
        const handleOffline = () => setIsConnected(false);

        window.addEventListener("online", handleOnline);
        window.addEventListener("offline", handleOffline);

        return () => {
            window.removeEventListener("online", handleOnline);
            window.removeEventListener("offline", handleOffline);
        };
    }, []);

    useEffect(() => {
        let cancelled = false;
        const controller = new AbortController();

        const fetchRoles = async () => {
            setRolesState((state) => ({ ...state, loading: true, error: null }));
            try {
                const response = await fetch(`${API_BASE}/api/roles${buildQueryString(filters)}`, {
                    signal: controller.signal,
                });
                if (!response.ok) {
                    throw new Error(`请求失败 (${response.status})`);
                }
                const data = await response.json();
                if (!cancelled) {
                    setRolesState({ data: Array.isArray(data) ? data : [], loading: false, error: null });
                    setIsConnected(true);
                }
            } catch (err) {
                if (cancelled || err.name === "AbortError") {
                    return;
                }
                setRolesState((state) => ({ ...state, loading: false, error: err.message || "加载角色失败" }));
                setIsConnected(false);
            }
        };

        fetchRoles();

        return () => {
            cancelled = true;
            controller.abort();
        };
    }, [filters.domain, filters.tags]);

    const refreshVoices = async () => {
        setVoicesState((state) => ({ ...state, loading: true, error: null }));
        try {
            const response = await fetch(`${API_BASE}/api/audio/voices`);
            if (!response.ok) {
                throw new Error(`请求失败 (${response.status})`);
            }
            const payload = await response.json();
            setVoicesState({ data: Array.isArray(payload?.voices) ? payload.voices : [], loading: false, error: null });
        } catch (err) {
            setVoicesState((state) => ({ ...state, loading: false, error: err.message || "获取音色列表失败" }));
        }
    };

    useEffect(() => {
        refreshVoices();
    }, []);

    const heroRoles = useMemo(() => rolesState.data.slice(0, 6), [rolesState.data]);

    const handleApplyFilters = (domain, tags) => {
        setFilters({ domain, tags });
    };

    const handleResetFilters = () => {
        setFilters({ domain: "", tags: "" });
    };

    return (
        <div className="app-shell">
            <header className="app-header">
                <div className="logo">AI 角色扮演</div>
                <div className="search-box">
                    <input type="search" placeholder="搜索角色、标签或领域…" />
                </div>
                <ConnectionIndicator connected={isConnected} />
                <button type="button" className="icon-button" aria-label="设置">
                    ⚙️
                </button>
            </header>

            <div className="app-body">
                <nav className="tab-bar" role="tablist">
                    {TABS.map((tab) => (
                        <button
                            key={tab.id}
                            type="button"
                            role="tab"
                            className={tab.id === activeTab ? "active" : ""}
                            aria-selected={tab.id === activeTab}
                            onClick={() => setActiveTab(tab.id)}
                        >
                            {tab.label}
                        </button>
                    ))}
                </nav>

                <section className="tab-content">
                    {activeTab === "home" && (
                        <HomeTab
                            roles={heroRoles}
                            loading={rolesState.loading}
                            error={rolesState.error}
                            onExplore={() => setActiveTab("roles")}
                            onStart={(roleId) => {
                                if (roleId) {
                                    setSelectedRoleId(roleId);
                                }
                                setActiveTab("chat");
                            }}
                        />
                    )}

                    {activeTab === "roles" && (
                        <RolesCatalog
                            roles={rolesState.data}
                            loading={rolesState.loading}
                            error={rolesState.error}
                            filters={filters}
                            onApplyFilters={handleApplyFilters}
                            onResetFilters={handleResetFilters}
                            onStartChat={(roleId) => {
                                setSelectedRoleId(roleId);
                                setActiveTab("chat");
                            }}
                        />
                    )}

                    {activeTab === "chat" && (
                        <VoiceChatPage
                            roles={rolesState.data}
                            selectedRoleId={selectedRoleId}
                            onSelectRole={setSelectedRoleId}
                            voices={voicesState.data}
                            voicesLoading={voicesState.loading}
                            voicesError={voicesState.error}
                            onRefreshVoices={refreshVoices}
                        />
                    )}

                    {activeTab === "history" && <HistoryTab />}
                    {activeTab === "settings" && <SettingsTab />}
                </section>
            </div>
        </div>
    );
};

export default App;
