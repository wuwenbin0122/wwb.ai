import React, { useEffect, useMemo, useState } from "react";

const API_BASE = process.env.REACT_APP_API_BASE || "";

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
                if (active) {
                    if (err.name !== "AbortError") {
                        setError(err.message || "Failed to load roles");
                    }
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

    return (
        <div style={{ padding: "24px" }}>
            <h1>Role Selection</h1>

            <section style={{ marginBottom: "16px" }}>
                <label style={{ display: "block", marginBottom: "8px" }}>
                    Domain:
                    <input
                        type="text"
                        value={domain}
                        onChange={(e) => setDomain(e.target.value)}
                        placeholder="e.g. Literature"
                        style={{ marginLeft: "8px" }}
                    />
                </label>
                <label style={{ display: "block" }}>
                    Tags:
                    <input
                        type="text"
                        value={tags}
                        onChange={(e) => setTags(e.target.value)}
                        placeholder="comma separated e.g. Brave, Wizard"
                        style={{ marginLeft: "8px", width: "280px" }}
                    />
                </label>
            </section>

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
