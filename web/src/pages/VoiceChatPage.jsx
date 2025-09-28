import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";
const TARGET_SAMPLE_RATE = 16000;
const WORKLET_URL = `/worklets/pcm-processor.js`;
const CHAT_HISTORY_LIMIT = 8;

const downsampleFloat32 = (input, inputSampleRate) => {
    if (!input || input.length === 0) {
        return null;
    }

    if (inputSampleRate === TARGET_SAMPLE_RATE) {
        return Int16Array.from(input, (sample) => {
            const s = Math.max(-1, Math.min(1, sample));
            return s < 0 ? s * 0x8000 : s * 0x7fff;
        });
    }

    const ratio = inputSampleRate / TARGET_SAMPLE_RATE;
    const newLength = Math.round(input.length / ratio);
    const result = new Int16Array(newLength);
    let offsetResult = 0;
    let offsetInput = 0;

    while (offsetResult < newLength) {
        const nextOffsetInput = Math.round((offsetResult + 1) * ratio);
        let accum = 0;
        let count = 0;
        for (let i = offsetInput; i < nextOffsetInput && i < input.length; i += 1) {
            accum += input[i];
            count += 1;
        }
        const sample = count > 0 ? accum / count : 0;
        const clamped = Math.max(-1, Math.min(1, sample));
        result[offsetResult] = clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff;
        offsetResult += 1;
        offsetInput = nextOffsetInput;
    }

    return result;
};

const base64ToUint8Array = (base64) => {
    if (!base64) {
        return new Uint8Array();
    }
    const binary = atob(base64);
    const length = binary.length;
    const bytes = new Uint8Array(length);
    for (let i = 0; i < length; i += 1) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
};

const mergeUint8Chunks = (chunks) => {
    if (!chunks || chunks.length === 0) {
        return new Uint8Array();
    }
    const total = chunks.reduce((acc, chunk) => acc + chunk.length, 0);
    const merged = new Uint8Array(total);
    let offset = 0;
    chunks.forEach((chunk) => {
        merged.set(chunk, offset);
        offset += chunk.length;
    });
    return merged;
};

const formatDuration = (ms) => {
    if (!ms && ms !== 0) {
        return "";
    }
    const seconds = Math.round(ms / 100) / 10;
    return `${seconds.toFixed(1)}s`;
};

const normalizeSkillList = (skills) => {
    if (!skills) {
        return [];
    }
    if (Array.isArray(skills)) {
        return skills
            .map((skill) => ({
                id: skill?.id ? String(skill.id).trim() : "",
                name: skill?.name ? String(skill.name).trim() : "",
            }))
            .filter((skill) => skill.id);
    }
    if (typeof skills === "string") {
        try {
            return normalizeSkillList(JSON.parse(skills));
        } catch (err) {
            return [];
        }
    }
    if (typeof skills === "object") {
        return normalizeSkillList(Object.values(skills));
    }
    return [];
};

const normalizeLanguageList = (languages) => {
    if (!languages) {
        return [];
    }
    if (Array.isArray(languages)) {
        return languages
            .map((lang) => (typeof lang === "string" ? lang.trim() : ""))
            .filter((lang) => lang !== "");
    }
    if (typeof languages === "string") {
        return languages
            .split(",")
            .map((lang) => lang.trim())
            .filter((lang) => lang !== "");
    }
    return [];
};

const VoiceChatPage = ({
    roles,
    selectedRoleId,
    onSelectRole,
    voices,
    voicesLoading,
    voicesError,
    onRefreshVoices,
}) => {
    const audioContextRef = useRef(null);
    const workletLoadedRef = useRef(false);
    const workletNodeRef = useRef(null);
    const mediaStreamRef = useRef(null);
    const asrSocketRef = useRef(null);
    const asrReadyRef = useRef(false);
    const asrQueuedChunksRef = useRef([]);
    const asrMessageIdRef = useRef(null);
    const asrAwaitingFinalRef = useRef(false);
    const asrActiveRef = useRef(false);
    const audioUrlsRef = useRef(new Set());
    const lastDiagRef = useRef({});
    const chatPendingRef = useRef(false);

    const [pendingStart, setPendingStart] = useState(false);
    const [isRecording, setIsRecording] = useState(false);
    const [error, setError] = useState(null);
    const [, setTranscripts] = useState([]);
    const [chatInput, setChatInput] = useState("");
    const [chatPending, setChatPending] = useState(false);
    const [chatError, setChatError] = useState(null);
    const [ttsPending, setTtsPending] = useState(false);
    const [ttsError, setTtsError] = useState(null);
    const [chatMessages, setChatMessages] = useState([]);
    // 技能开关已移除
    const [selectedLanguage, setSelectedLanguage] = useState("zh");

    const [audioUrl, setAudioUrl] = useState("");
    const audioPlayerRef = useRef(null);

    const [selectedVoice, setSelectedVoice] = useState("");
    const [speechSpeed, setSpeechSpeed] = useState(1.0);

    useEffect(() => {
        if (!selectedVoice && voices && voices.length > 0) {
            setSelectedVoice(voices[0].voice_type);
        }
    }, [voices, selectedVoice]);

    const selectedRole = useMemo(() => roles.find((role) => role.id === selectedRoleId) || null, [roles, selectedRoleId]);

    const roleLanguages = useMemo(() => normalizeLanguageList(selectedRole?.languages), [selectedRole]);
    const roleSkills = useMemo(() => normalizeSkillList(selectedRole?.skills), [selectedRole]);

    useEffect(() => {
        if (!selectedRole) {
            setSelectedLanguage("zh");
            return;
        }

        if (roleLanguages.length > 0) {
            setSelectedLanguage(roleLanguages[0]);
        } else {
            setSelectedLanguage("zh");
        }

    }, [selectedRole, roleLanguages]);

    // 技能开关已移除

    const createMessageId = useCallback(
        (prefix) => `${prefix}-${Date.now().toString(36)}-${Math.random().toString(16).slice(2, 8)}`,
        []
    );

    const cleanupRecording = useCallback(() => {
        if (workletNodeRef.current) {
            workletNodeRef.current.port.postMessage({ type: "STOP" });
            workletNodeRef.current.disconnect();
            workletNodeRef.current = null;
        }
        if (mediaStreamRef.current) {
            mediaStreamRef.current.getTracks().forEach((track) => track.stop());
            mediaStreamRef.current = null;
        }
        if (audioContextRef.current) {
            audioContextRef.current.close();
            audioContextRef.current = null;
        }
        workletLoadedRef.current = false;
    }, []);

    const teardownAsrSocket = useCallback(() => {
        const socket = asrSocketRef.current;
        if (socket) {
            try {
                socket.onopen = null;
                socket.onmessage = null;
                socket.onerror = null;
                socket.onclose = null;
                socket.close();
            } catch (err) {
                console.warn("[ASR] 关闭通道失败:", err);
            }
        }
        asrSocketRef.current = null;
        asrReadyRef.current = false;
        asrQueuedChunksRef.current = [];
        asrMessageIdRef.current = null;
        asrAwaitingFinalRef.current = false;
        asrActiveRef.current = false;
    }, []);

    useEffect(
        () => () => {
            cleanupRecording();
            teardownAsrSocket();
        },
        [cleanupRecording, teardownAsrSocket]
    );

    const ensureAudioContext = useCallback(async () => {
        if (!audioContextRef.current) {
            audioContextRef.current = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: TARGET_SAMPLE_RATE });
            workletLoadedRef.current = false;
        }

        const audioContext = audioContextRef.current;

        if (!audioContext.audioWorklet) {
            throw new Error("当前浏览器不支持 AudioWorklet");
        }

        if (!workletLoadedRef.current) {
            await audioContext.audioWorklet.addModule(WORKLET_URL);
            workletLoadedRef.current = true;
        }

        if (!workletNodeRef.current) {
            const workletNode = new AudioWorkletNode(audioContext, "pcm-processor");

            workletNode.port.onmessage = (event) => {
                const { data } = event;
                if (!data || data.type !== "PCM" || !data.payload) {
                    return;
                }

                const floatBuffer = new Float32Array(data.payload);
                if (!floatBuffer.length) {
                    return;
                }

                const sourceRate = typeof data.sampleRate === "number" && data.sampleRate > 0 ? data.sampleRate : audioContext.sampleRate;
                const resampled = downsampleFloat32(floatBuffer, sourceRate);
                if (!resampled || resampled.length === 0) {
                    return;
                }

                if (!asrActiveRef.current) {
                    return;
                }

                const buffer = resampled.buffer.slice(resampled.byteOffset, resampled.byteOffset + resampled.byteLength);
                const socket = asrSocketRef.current;
                if (socket && socket.readyState === WebSocket.OPEN && asrReadyRef.current) {
                    try {
                        socket.send(buffer);
                    } catch (sendErr) {
                        console.error("[ASR] 音频分片发送失败:", sendErr);
                    }
                } else {
                    asrQueuedChunksRef.current.push(buffer);
                }
            };
            workletNodeRef.current = workletNode;
        }

        return audioContext;
    }, []);

    useEffect(
        () => () => {
            audioUrlsRef.current.forEach((url) => URL.revokeObjectURL(url));
            audioUrlsRef.current.clear();
        },
        []
    );

    const synthesizeAndPlay = useCallback(
        async (text) => {
            const trimmed = text.trim();
            if (trimmed === "") {
                return null;
            }

            const response = await fetch(`${API_BASE}/api/audio/tts`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    text: trimmed,
                    voice_type: selectedVoice || undefined,
                    encoding: "mp3",
                    speed_ratio: speechSpeed,
                }),
            });

            const data = await response.json();
            if (!response.ok) {
                throw new Error(data.detail || data.error || "TTS 请求失败");
            }

            const audioBase64 = data.audio || "";
            if (!audioBase64) {
                throw new Error("未返回音频内容");
            }

            const merged = mergeUint8Chunks([base64ToUint8Array(audioBase64)]);
            const blob = new Blob([merged], { type: "audio/mpeg" });
            const url = URL.createObjectURL(blob);
            audioUrlsRef.current.add(url);
            setAudioUrl(url);

            requestAnimationFrame(() => {
                if (audioPlayerRef.current) {
                    audioPlayerRef.current.load();
                    audioPlayerRef.current.play().catch(() => {});
                }
            });

            return { url, duration: data.duration, reqid: data.reqid };
        },
        [selectedVoice, speechSpeed]
    );

    const sendChatMessage = useCallback(
        async (text, options = {}) => {
            const trimmed = text.trim();
            if (trimmed === "") {
                return false;
            }

            if (!selectedRole || !selectedRoleId) {
                setChatError("请先选择角色");
                return false;
            }

            if (chatPendingRef.current) {
                setChatError("上一轮对话仍在处理中，请稍候…");
                return false;
            }

            setChatError(null);
            setTtsError(null);
            chatPendingRef.current = true;
            setChatPending(true);

            const mergeIntoId = typeof options.mergeIntoId === "string" ? options.mergeIntoId : "";
            const targetId = mergeIntoId || createMessageId("user");
            const userMessage = {
                id: targetId,
                role: "user",
                content: trimmed,
                metadata: options.userMetadata,
            };

            if (mergeIntoId) {
                setChatMessages((prev) => {
                    let found = false;
                    const updated = prev.map((msg) => {
                        if (msg.id !== targetId) {
                            return msg;
                        }
                        found = true;
                        return {
                            ...msg,
                            content: trimmed,
                            metadata: options.userMetadata ?? msg.metadata,
                        };
                    });
                    if (!found) {
                        return [...updated, userMessage];
                    }
                    return updated;
                });
            } else {
                setChatMessages((prev) => [...prev, userMessage]);
            }

            const historyWithCurrent = (() => {
                if (mergeIntoId) {
                    let replaced = false;
                    const adjusted = chatMessages.map((msg) => {
                        if (msg.id === targetId) {
                            replaced = true;
                            return { ...msg, content: trimmed };
                        }
                        return msg;
                    });
                    if (!replaced) {
                        adjusted.push(userMessage);
                    }
                    return adjusted;
                }
                return [...chatMessages, userMessage];
            })();
            const trimmedHistory = historyWithCurrent.slice(-CHAT_HISTORY_LIMIT).map((msg) => ({
                role: msg.role === "assistant" ? "assistant" : "user",
                content: msg.content,
            }));
            const previousHistory = trimmedHistory.slice(0, -1);

            try {
                const payload = {
                    role_id: selectedRoleId,
                    roleId: selectedRoleId,
                    language: selectedLanguage,
                    lang: selectedLanguage,
                    messages: trimmedHistory,
                    history: previousHistory,
                    text: trimmed,
                };

                const response = await fetch(`${API_BASE}/api/nlp/chat`, {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(payload),
                });

                const data = await response.json();
                if (!response.ok) {
                    throw new Error(data.detail || data.error || "NLP 请求失败");
                }

                const replyPayload = data.message || data.reply || {};
                const assistantText = typeof replyPayload.content === "string" ? replyPayload.content.trim() : "";
                const assistantId = createMessageId("assistant");
                const assistantMessage = {
                    id: assistantId,
                    role: "assistant",
                    content: assistantText || "（未返回内容）",
                };
                setChatMessages((prev) => [...prev, assistantMessage]);

                if (assistantText) {
                    setTtsPending(true);
                    try {
                        const audioMeta = await synthesizeAndPlay(assistantText);
                        if (audioMeta) {
                            setChatMessages((prev) =>
                                prev.map((msg) => (msg.id === assistantId ? { ...msg, audio: audioMeta } : msg))
                            );
                        }
                    } catch (ttsErr) {
                        setTtsError(ttsErr.message || "TTS 请求失败");
                    } finally {
                        setTtsPending(false);
                    }
                }

                return true;
            } catch (err) {
                setChatError(err.message || "发送失败");
                return false;
            } finally {
                chatPendingRef.current = false;
                setChatPending(false);
            }
        },
        [chatMessages, createMessageId, selectedLanguage, selectedRole, selectedRoleId, synthesizeAndPlay]
    );

    const handleAsrTranscript = useCallback(
        async (payload) => {
            if (!payload || typeof payload !== "object") {
                return;
            }

            const rawText = typeof payload.text === "string" ? payload.text : "";
            const text = rawText.trim();
            const isFinal = Boolean(
                payload.is_final ||
                    payload.isFinal ||
                    payload.final ||
                    (typeof payload.state === "string" && payload.state.toLowerCase() === "final")
            );
            const durationField =
                typeof payload.duration_ms === "number"
                    ? payload.duration_ms
                    : typeof payload.duration === "number"
                    ? payload.duration
                    : 0;
            const durationMs = Number.isFinite(durationField) ? Math.max(0, Math.round(durationField)) : 0;

            if (!text && !isFinal) {
                return;
            }

            setError(null);

            if (text) {
                setTranscripts((prev) => [...prev, { text, isFinal, timestamp: Date.now() }]);
            }

            let messageId = asrMessageIdRef.current;
            const metadata = durationMs > 0 ? { duration: formatDuration(durationMs) } : undefined;

            if (!messageId && text) {
                messageId = createMessageId("user");
                asrMessageIdRef.current = messageId;
                setChatMessages((prev) => [...prev, { id: messageId, role: "user", content: text, metadata }]);
            } else if (messageId) {
                setChatMessages((prev) =>
                    prev.map((msg) => {
                        if (msg.id !== messageId) {
                            return msg;
                        }
                        return {
                            ...msg,
                            content: text || msg.content,
                            metadata: metadata ?? msg.metadata,
                        };
                    })
                );
            }

            if (isFinal) {
                try {
                    if (text) {
                        await sendChatMessage(text, {
                            userMetadata: metadata,
                            mergeIntoId: messageId || undefined,
                        });
                    }
                } finally {
                    asrMessageIdRef.current = null;
                    if (asrAwaitingFinalRef.current) {
                        asrAwaitingFinalRef.current = false;
                        teardownAsrSocket();
                    }
                }
            }
        },
        [createMessageId, sendChatMessage, setError, setTranscripts, teardownAsrSocket]
    );

    const handleAsrPayload = useCallback(
        (message) => {
            if (!message || typeof message !== "object") {
                return;
            }

            const type = typeof message.type === "string" ? message.type : "";
            switch (type) {
                case "transcript":
                    handleAsrTranscript(message).catch((err) => {
                        console.error("[ASR] 处理转写失败:", err);
                        setError(err.message || "ASR 处理失败");
                    });
                    break;
                case "error": {
                    const detail =
                        typeof message.detail === "string"
                            ? message.detail
                            : typeof message.error === "string"
                            ? message.error
                            : "ASR 通道错误";
                    setError(detail);
                    teardownAsrSocket();
                    break;
                }
                case "pong":
                case "ready":
                    break;
                case "upstream":
                    if (message.payload) {
                        console.debug?.("[ASR] upstream:", message.payload);
                    }
                    break;
                default:
                    break;
            }
        },
        [handleAsrTranscript, setError, teardownAsrSocket]
    );

    const setupAsrSocket = useCallback(() => {
        return new Promise((resolve, reject) => {
            try {
                const base = API_BASE && API_BASE.trim() ? API_BASE : window.location.origin;
                const url = new URL("/ws/audio/asr", base);
                url.protocol = url.protocol === "https:" ? "wss:" : "ws:";

                const socket = new WebSocket(url);
                socket.binaryType = "arraybuffer";
                let resolved = false;

                asrReadyRef.current = false;
                asrSocketRef.current = socket;

                const fulfill = () => {
                    if (!resolved) {
                        resolved = true;
                        resolve(socket);
                    }
                };

                socket.onopen = () => {
                    socket.send(
                        JSON.stringify({
                            type: "start",
                            sampleRate: TARGET_SAMPLE_RATE,
                            channels: 1,
                            bits: 16,
                        })
                    );
                };

                socket.onmessage = (event) => {
                    if (typeof event.data !== "string") {
                        return;
                    }
                    let payload;
                    try {
                        payload = JSON.parse(event.data);
                    } catch {
                        return;
                    }
                    if (!payload || typeof payload !== "object") {
                        return;
                    }
                    if (payload.type === "ready") {
                        asrReadyRef.current = true;
                        if (asrQueuedChunksRef.current.length > 0) {
                            const pending = asrQueuedChunksRef.current.slice();
                            asrQueuedChunksRef.current = [];
                            pending.forEach((chunk) => {
                                try {
                                    socket.send(chunk);
                                } catch (err) {
                                    console.error("[ASR] 队列音频发送失败:", err);
                                }
                            });
                        }
                        fulfill();
                        return;
                    }
                    handleAsrPayload(payload);
                };

                socket.onerror = () => {
                    if (!resolved) {
                        reject(new Error("ASR 通道建立失败"));
                    }
                    setError("ASR 通道发生错误");
                };

                socket.onclose = () => {
                    if (!resolved) {
                        reject(new Error("ASR 通道已关闭"));
                    }
                    if (asrSocketRef.current === socket) {
                        teardownAsrSocket();
                    }
                };
            } catch (err) {
                reject(err);
            }
        });
    }, [handleAsrPayload, setError, teardownAsrSocket]);

    const startRecording = useCallback(async () => {
        if (pendingStart || isRecording) {
            return;
        }

        if (!selectedRole || !selectedRoleId) {
            setError("请先选择角色");
            return;
        }

        if (chatPendingRef.current) {
            setError("上一轮对话仍在处理中，请稍候…");
            return;
        }

        setError(null);
        setPendingStart(true);

        try {
            const audioContext = await ensureAudioContext();
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            mediaStreamRef.current = stream;

            const source = audioContext.createMediaStreamSource(stream);
            const workletNode = workletNodeRef.current;
            if (!workletNode) {
                throw new Error("Audio processing node unavailable");
            }

            if (asrSocketRef.current) {
                teardownAsrSocket();
            }

            const socket = await setupAsrSocket();
            if (!socket || socket.readyState !== WebSocket.OPEN) {
                throw new Error("ASR 通道未就绪");
            }

            asrActiveRef.current = true;
            asrAwaitingFinalRef.current = false;

            await audioContext.resume();

            source.connect(workletNode);
            workletNode.connect(audioContext.destination);
            setIsRecording(true);
        } catch (err) {
            setError(err.message || "无法启动录音");
            cleanupRecording();
            teardownAsrSocket();
        } finally {
            setPendingStart(false);
        }
    }, [
        chatPendingRef,
        cleanupRecording,
        ensureAudioContext,
        isRecording,
        pendingStart,
        selectedRole,
        selectedRoleId,
        setupAsrSocket,
        teardownAsrSocket,
    ]);

    const stopRecording = useCallback(async () => {
        if (!isRecording) {
            return;
        }

        const socket = asrSocketRef.current;
        asrActiveRef.current = false;

        if (socket && socket.readyState === WebSocket.OPEN) {
            try {
                socket.send(JSON.stringify({ type: "stop" }));
                asrAwaitingFinalRef.current = true;
            } catch (err) {
                console.error("[ASR] 发送停止指令失败:", err);
                asrAwaitingFinalRef.current = false;
            }
        } else {
            asrAwaitingFinalRef.current = false;
            teardownAsrSocket();
        }

        cleanupRecording();
        setIsRecording(false);
    }, [cleanupRecording, isRecording, teardownAsrSocket]);

    const handleSendChat = useCallback(async () => {
        const text = chatInput.trim();
        if (text === "") {
            return;
        }

        const success = await sendChatMessage(text);
        if (success) {
            setChatInput("");
        }
    }, [chatInput, sendChatMessage]);

    const groupedVoices = useMemo(() => {
        if (!voices || voices.length === 0) {
            return [];
        }

        const map = new Map();
        voices.forEach((voice) => {
            const key = voice.category || "默认";
            if (!map.has(key)) {
                map.set(key, []);
            }
            map.get(key).push(voice);
        });

        return Array.from(map.entries());
    }, [voices]);

    return (
        <div className="chat-layout">
            <aside className="chat-sidebar">
                <div className="sidebar-header">
                    <h3>角色与会话</h3>
                    <p className="muted">选择角色以加载预设语气。</p>
                </div>
                <div className="sidebar-list">
                    {roles.map((role) => (
                        <button
                            key={role.id}
                            type="button"
                            className={role.id === selectedRoleId ? "selected" : ""}
                            onClick={() => onSelectRole(role.id)}
                        >
                            <span className="avatar" aria-hidden="true">
                                {String(role?.name ?? "?").slice(0, 2)}
                            </span>
                            <span>{role.name}</span>
                        </button>
                    ))}
                    {roles.length === 0 && <p className="muted">暂无角色，请先在角色目录中添加。</p>}
                </div>
            </aside>

            <div className="chat-main">
                <header className="chat-main-header">
                    <div>
                        <h2>{selectedRole ? selectedRole.name : "选择一个角色开始对话"}</h2>
                        <p className="muted">
                            {selectedRole
                                ? selectedRole.bio || "这位角色还没有简介。"
                                : "点击左侧角色列表或在发现页中选择角色即可开始。"}
                        </p>
                    </div>
                    <div className={`record-indicator ${isRecording ? "recording" : "idle"}`}>
                        <span className="dot" />
                        {isRecording ? "录音中" : "待命"}
                    </div>
                </header>

                <div className="chat-transcript" role="log">
                    {chatMessages.length === 0 && <p className="muted">记录你的语音或文本，将在这里呈现实时字幕与回复。</p>}
                    {chatPending && (
                        <div className="chat-bubble assistant">
                            <div className="bubble-content">
                                <span className="typing">● ● ●</span>
                            </div>
                        </div>
                    )}
                    {chatMessages.map((message) => (
                        <div key={message.id} className={`chat-bubble ${message.role}`}>
                            <div className="bubble-content">
                                <p>{message.content}</p>
                                {message.metadata?.duration && (
                                    <span className="bubble-meta">{message.metadata.duration}</span>
                                )}
                                {message.audio && (
                                    <audio controls src={message.audio.url} />
                                )}
                            </div>
                        </div>
                    ))}
                </div>

                <div className="chat-input">
                    <div className="record-controls">
                        <button type="button" className="primary" onClick={startRecording} disabled={pendingStart || isRecording}>
                            {pendingStart ? "准备中…" : isRecording ? "录音中" : "开始录音"}
                        </button>
                        <button type="button" className="ghost" onClick={stopRecording} disabled={!isRecording}>
                            结束录音
                        </button>
                    </div>

                    <div className="text-controls">
                        <textarea
                            rows={2}
                            value={chatInput}
                            onChange={(event) => setChatInput(event.target.value)}
                            placeholder={selectedRole ? "输入文本与角色对话，或使用上方录音按钮。" : "请先选择角色后再开始对话。"}
                            disabled={chatPending || ttsPending || !selectedRole}
                        />
                        <button
                            type="button"
                            className="primary"
                            onClick={handleSendChat}
                            disabled={chatPending || ttsPending || !selectedRole}
                        >
                            {chatPending || ttsPending ? "处理中…" : "发送"}
                        </button>
                    </div>

                    {(error || chatError || ttsError) && (
                        <div className="error-block">
                            {error && <p>ASR：{error}</p>}
                            {chatError && <p>Chat：{chatError}</p>}
                            {ttsError && <p>TTS：{ttsError}</p>}
                        </div>
                    )}
                </div>

                <footer className="chat-footer">
                    <audio controls ref={audioPlayerRef}>
                        {audioUrl && <source src={audioUrl} type="audio/mpeg" />}
                        您的浏览器不支持 audio 元素。
                    </audio>
                    <button
                        type="button"
                        className="ghost"
                        onClick={async () => {
                            const info = {
                                ...lastDiagRef.current,
                                voice: selectedVoice,
                                speed: speechSpeed,
                            };
                            const text = JSON.stringify(info, null, 2);
                            try {
                                await navigator.clipboard.writeText(text);
                                console.log("copied diagnostics", info);
                            } catch (e) {
                                console.log(text);
                            }
                        }}
                        style={{ marginLeft: 12 }}
                    >
                        复制诊断信息
                    </button>
                </footer>
            </div>

            <aside className="chat-settings">
                <div className="settings-section">
                    <h3>语言</h3>
                    {roleLanguages.length > 0 ? (
                        <select value={selectedLanguage} onChange={(event) => setSelectedLanguage(event.target.value)}>
                            {roleLanguages.map((lang) => (
                                <option key={lang} value={lang}>
                                    {lang}
                                </option>
                            ))}
                        </select>
                    ) : (
                        <p className="muted">该角色未提供语言信息，默认使用中文。</p>
                    )}
                </div>

                {/* 技能开关移除 */}

                <div className="settings-section">
                    <h3>音色与语速</h3>
                    <p className="muted">从音色列表中选择喜爱的声音，并调整语速。</p>
                    <div className="voice-select">
                        <label htmlFor="voice-select">音色</label>
                        <select
                            id="voice-select"
                            value={selectedVoice}
                            onChange={(event) => setSelectedVoice(event.target.value)}
                            disabled={voicesLoading || !voices || voices.length === 0}
                        >
                            {groupedVoices.map(([category, group]) => (
                                <optgroup key={category} label={category}>
                                    {group.map((voice) => (
                                        <option key={voice.voice_type} value={voice.voice_type}>
                                            {voice.voice_name || voice.voice_type}
                                        </option>
                                    ))}
                                </optgroup>
                            ))}
                        </select>
                    </div>

                    <label className="slider-label" htmlFor="speed-slider">
                        语速：{speechSpeed.toFixed(1)}x
                    </label>
                    <input
                        id="speed-slider"
                        type="range"
                        min="0.5"
                        max="1.8"
                        step="0.1"
                        value={speechSpeed}
                        onChange={(event) => setSpeechSpeed(parseFloat(event.target.value))}
                    />
                </div>

                <div className="settings-section">
                    <div className="settings-header">
                        <h3>音色库</h3>
                        <button type="button" className="ghost" onClick={onRefreshVoices} disabled={voicesLoading}>
                            刷新
                        </button>
                    </div>
                    {voicesLoading && <p className="muted">正在加载音色…</p>}
                    {voicesError && <p className="error">{voicesError}</p>}
                    {!voicesLoading && !voicesError && voices && voices.length > 0 && (
                        <ul className="voice-list">
                            {voices.slice(0, 5).map((voice) => (
                                <li key={voice.voice_type}>
                                    <div>
                                        <strong>{voice.voice_name || voice.voice_type}</strong>
                                        <p className="muted">{voice.category || "默认分类"}</p>
                                    </div>
                                    <a href={voice.url} target="_blank" rel="noreferrer">
                                        试听
                                    </a>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
            </aside>
        </div>
    );
};

export default VoiceChatPage;
