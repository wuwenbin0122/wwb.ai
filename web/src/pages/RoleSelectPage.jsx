import React, { useEffect, useMemo, useState } from "react";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";

const buildQuery = (domain, tags) => {
    const params = new URLSearchParams();
    if (domain.trim() !== "") {
        params.append("domain", domain.trim());
    }
    if (tags.trim() !== "") {
        params.append("tags", tags.trim());
    }
    const query = params.toString();
    return query ? `?${query}` : "";
};

const RoleSelectPage = () => {
    const [roles, setRoles] = useState([]);
    const [domainInput, setDomainInput] = useState("");
    const [tagsInput, setTagsInput] = useState("");
    const [domain, setDomain] = useState("");
    const [tags, setTags] = useState("");
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);

    const requestUrl = useMemo(() => `${API_BASE}/api/roles${buildQuery(domain, tags)}`, [domain, tags]);

    useEffect(() => {
        let active = true;
        const controller = new AbortController();

        const fetchRoles = async () => {
            setLoading(true);
            setError(null);
            try {
                const response = await fetch(requestUrl, { signal: controller.signal });
                if (!response.ok) {
                    throw new Error(`Request failed with status ${response.status}`);
                }
                const data = await response.json();
                if (active) {
                    setRoles(Array.isArray(data) ? data : [data]);
                }
            } catch (err) {
                if (active && err.name !== "AbortError") {
                    setError(err.message || "Failed to load roles");
                }
            } finally {
                if (active) {
                    setLoading(false);
                }
            }
        };

        fetchRoles();

        return () => {
            active = false;
            controller.abort();
        };
    }, [requestUrl]);

    const handleSubmit = (event) => {
        event.preventDefault();
        setDomain(domainInput.trim());
        setTags(tagsInput.trim());
    };

    const handleReset = () => {
        setDomainInput("");
        setTagsInput("");
        setDomain("");
        setTags("");
    };

    const hasActiveFilters = domain.trim() !== "" || tags.trim() !== "";

    return (
        <div style={{ padding: "24px" }}>
            <h1>Role Selection</h1>

            <form
                onSubmit={handleSubmit}
                style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: "12px",
                    maxWidth: "420px",
                    marginBottom: "20px",
                }}
            >
                <label style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                    Domain
                    <input
                        type="text"
                        value={domainInput}
                        onChange={(e) => setDomainInput(e.target.value)}
                        placeholder="e.g. Literature"
                    />
                </label>
                <label style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                    Tags
                    <input
                        type="text"
                        value={tagsInput}
                        onChange={(e) => setTagsInput(e.target.value)}
                        placeholder="comma separated e.g. Brave, Wizard"
                    />
                </label>
                <div style={{ display: "flex", gap: "12px" }}>
                    <button type="submit">Apply Filters</button>
                    <button type="button" onClick={handleReset}>
                        Reset
                    </button>
                </div>
            </form>

            {hasActiveFilters && (
                <div style={{ marginBottom: "16px" }}>
                    <strong>Active filters:</strong> {domain && `Domain = ${domain}`} {domain && tags && "|"}{" "}
                    {tags && `Tags contain ${tags}`}
                </div>
            )}

            {loading && <p>Loading roles...</p>}
            {error && <p style={{ color: "red" }}>Error: {error}</p>}

            {!loading && !error && roles.length === 0 && <p>No roles found.</p>}

            <ul style={{ listStyle: "none", padding: 0 }}>
                {roles.map((role) => (
                    <li
                        key={role.id}
                        style={{
                            border: "1px solid #ccc",
                            borderRadius: "8px",
                            padding: "12px",
                            marginBottom: "12px",
                        }}
                    >
                        <h2 style={{ margin: "0 0 8px" }}>{role.name}</h2>
                        <p style={{ margin: "0 0 4px" }}>
                            <strong>Domain:</strong> {role.domain || "Unknown"}
                        </p>
                        <p style={{ margin: "0 0 4px" }}>
                            <strong>Tags:</strong> {role.tags || "None"}
                        </p>
                        <p style={{ margin: 0 }}>{role.bio || "No bio available."}</p>
                    </li>
                ))}
            </ul>
        </div>
    );
};

export default RoleSelectPage;
