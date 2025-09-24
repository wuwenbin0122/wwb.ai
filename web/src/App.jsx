import React, { useState } from "react";
import RoleSelectPage from "./pages/RoleSelectPage.jsx";
import VoiceChatPage from "./pages/VoiceChatPage.jsx";

const App = () => {
    const [activeTab, setActiveTab] = useState("roles");

    return (
        <main className="app-shell">
            <nav className="app-nav">
                <button
                    type="button"
                    className={activeTab === "roles" ? "active" : ""}
                    onClick={() => setActiveTab("roles")}
                >
                    Roles
                </button>
                <button
                    type="button"
                    className={activeTab === "voice" ? "active" : ""}
                    onClick={() => setActiveTab("voice")}
                >
                    Voice Chat
                </button>
            </nav>
            <section className="app-content">
                {activeTab === "roles" ? <RoleSelectPage /> : <VoiceChatPage />}
            </section>
        </main>
    );
};

export default App;
