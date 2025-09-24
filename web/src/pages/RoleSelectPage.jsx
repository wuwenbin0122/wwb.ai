import React, { useEffect, useState } from "react";

const RolesCatalog = ({
    roles,
    loading,
    error,
    filters,
    onApplyFilters,
    onResetFilters,
    onStartChat,
}) => {
    const [domainInput, setDomainInput] = useState(filters.domain ?? "");
    const [tagsInput, setTagsInput] = useState(filters.tags ?? "");

    useEffect(() => {
        setDomainInput(filters.domain ?? "");
    }, [filters.domain]);

    useEffect(() => {
        setTagsInput(filters.tags ?? "");
    }, [filters.tags]);

    const handleSubmit = (event) => {
        event.preventDefault();
        onApplyFilters(domainInput.trim(), tagsInput.trim());
    };

    const hasActiveFilters = filters.domain.trim() !== "" || filters.tags.trim() !== "";

    const renderFilters = () => (
        <aside className="filter-panel">
            <h3>筛选条件</h3>
            <form onSubmit={handleSubmit} className="filter-form">
                <label>
                    领域
                    <input
                        type="text"
                        value={domainInput}
                        onChange={(event) => setDomainInput(event.target.value)}
                        placeholder="例如：Philosophy"
                    />
                </label>
                <label>
                    标签
                    <input
                        type="text"
                        value={tagsInput}
                        onChange={(event) => setTagsInput(event.target.value)}
                        placeholder="用逗号分隔，如：勇敢, 魔法"
                    />
                </label>
                <div className="filter-actions">
                    <button type="submit" className="primary" disabled={loading}>
                        应用筛选
                    </button>
                    <button type="button" className="ghost" onClick={onResetFilters}>
                        重置
                    </button>
                </div>
            </form>

            {hasActiveFilters && (
                <div className="active-filters">
                    <h4>当前筛选</h4>
                    <div className="chips">
                        {filters.domain.trim() !== "" && <span className="chip">领域：{filters.domain}</span>}
                        {filters.tags
                            .split(",")
                            .map((tag) => tag.trim())
                            .filter(Boolean)
                            .map((tag) => (
                                <span key={tag} className="chip">
                                    标签：{tag}
                                </span>
                            ))}
                    </div>
                </div>
            )}
        </aside>
    );

    const renderContent = () => (
        <div className="catalog-results">
            <div className="catalog-header">
                <div>
                    <h2>角色目录</h2>
                    <p className="muted">根据主题、语种或标签寻找最适合的 AI 伙伴。</p>
                </div>
                <span className="result-count">{roles.length} 个角色</span>
            </div>

            {loading && <p className="muted">正在加载角色…</p>}
            {error && <p className="error">{error}</p>}
            {!loading && !error && roles.length === 0 && <p className="muted">没有符合条件的角色。</p>}

            <div className="role-grid">
                {roles.map((role) => (
                    <button key={role.id} type="button" className="role-card" onClick={() => onStartChat(role.id)}>
                        <div className="role-avatar" aria-hidden="true">
                            {role.name.slice(0, 2)}
                        </div>
                        <div className="role-meta">
                            <h3>{role.name}</h3>
                            <p>{role.bio || "暂无简介"}</p>
                            <div className="tags">
                                {(role.tags || "")
                                    .split(",")
                                    .map((tag) => tag.trim())
                                    .filter(Boolean)
                                    .slice(0, 4)
                                    .map((tag) => (
                                        <span key={tag} className="tag">
                                            {tag}
                                        </span>
                                    ))}
                            </div>
                        </div>
                    </button>
                ))}
            </div>
        </div>
    );

    return (
        <div className="roles-catalog">
            {renderFilters()}
            {renderContent()}
        </div>
    );
};

export default RolesCatalog;
