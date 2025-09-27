import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";
const TARGET_SAMPLE_RATE = 16000;
const WORKLET_URL = `/worklets/pcm-processor.js`;

const buildWebSocketURL = (path) => {
    const base = API_BASE && API_BASE.trim() !== "" ? API_BASE : window.location.origin;
    const url = new URL(path, base);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.toString();
};

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
    const recordedChunksRef = useRef([]);
    const asrSocketRef = useRef(null);
    const streamingActiveRef = useRef(false);

    const [pendingStart, setPendingStart] = useState(false);
    const [isRecording, setIsRecording] = useState(false);
    const [error, setError] = useState(null);
    const [transcripts, setTranscripts] = useState([]);
    const [liveTranscript, setLiveTranscript] = useState("");
    const [ttsText, setTtsText] = useState("");
    const [ttsPending, setTtsPending] = useState(false);
    const [ttsError, setTtsError] = useState(null);
    const [chatMessages, setChatMessages] = useState([]);

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

    const closeASRSocket = useCallback((code = 1000, reason = "") => {
        streamingActiveRef.current = false;
        setLiveTranscript("");
        const socket = asrSocketRef.current;
        if (!socket) {
            asrSocketRef.current = null;
            return;
        }
        asrSocketRef.current = null;
        try {
            socket.onopen = null;
            socket.onmessage = null;
            socket.onerror = null;
            socket.onclose = null;
            if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
                socket.close(code, reason);
            }
        } catch (err) {
            // ignore close errors
        }
    }, [setLiveTranscript]);

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

    useEffect(
        () => () => {
            cleanupRecording();
            closeASRSocket(1000, "teardown");
        },
        [cleanupRecording, closeASRSocket],
    );

    const sendPCMChunk = useCallback(
        (chunk) => {
            if (!chunk || chunk.length === 0) {
                return;
            }
            const socket = asrSocketRef.current;
            if (!socket || socket.readyState !== WebSocket.OPEN || !streamingActiveRef.current) {
                return;
            }
            try {
                const buffer = chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.byteLength);
                socket.send(buffer);
            } catch (err) {
                const message = err instanceof Error ? err.message : "实时音频发送失败";
                setError(message);
                closeASRSocket(1011, "chunk-send-error");
            }
        },
        [closeASRSocket, setError],
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
            recordedChunksRef.current = [];

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
                if (resampled && resampled.length > 0) {
                    recordedChunksRef.current.push(resampled);
                    sendPCMChunk(resampled);
                }
            };
            workletNodeRef.current = workletNode;
        }

        return audioContext;
    }, [sendPCMChunk]);

    const openASRSocket = useCallback(() => {
        return new Promise((resolve, reject) => {
            try {
                const socket = new WebSocket(buildWebSocketURL("/api/audio/asr/stream"));
                socket.binaryType = "arraybuffer";
                asrSocketRef.current = socket;

                let handshakeResolved = false;

                const fail = (message) => {
                    if (!handshakeResolved) {
                        handshakeResolved = true;
                        reject(new Error(message));
                    } else {
                        setError(message);
                    }
                    closeASRSocket(1011, message);
                };

                socket.onopen = () => {
                    try {
                        socket.send(
                            JSON.stringify({
                                type: "start",
                                audio_format: "pcm",
                                sample_rate: TARGET_SAMPLE_RATE,
                                channels: 1,
                                bits: 16,
                            }),
                        );
                    } catch (err) {
                        fail(err instanceof Error ? err.message : "初始化 ASR 流失败");
                    }
                };

                socket.onmessage = (event) => {
                    let payload;
                    try {
                        payload = JSON.parse(event.data);
                    } catch (err) {
                        return;
                    }

                    const messageType = typeof payload.type === "string" ? payload.type.toLowerCase() : "";

                    if (!handshakeResolved) {
                        if (messageType === "ready") {
                            handshakeResolved = true;
                            streamingActiveRef.current = true;
                            setLiveTranscript("");
                            resolve(socket);
                            return;
                        }
                        if (messageType === "error") {
                            fail(payload.error || "ASR 流初始化失败");
                            return;
                        }
                    }

                    if (messageType === "partial") {
                        setLiveTranscript(payload.text || "");
                        return;
                    }

                    if (messageType === "final") {
                        streamingActiveRef.current = false;
                        setLiveTranscript("");
                        const transcriptText = payload.text || "";
                        const duration = payload.duration_ms;
                        const reqid = payload.reqid;
                        if (transcriptText) {
                            setTranscripts((prev) => [...prev, { text: transcriptText, reqid, duration }]);
                            setChatMessages((prev) => [
                                ...prev,
                                {
                                    id: `user-${Date.now()}`,
                                    role: "user",
                                    content: transcriptText,
                                    metadata: duration ? { duration: formatDuration(duration) } : undefined,
                                },
                            ]);
                        }
                        cleanupRecording();
                        setIsRecording(false);
                        closeASRSocket(1000, "completed");
                        return;
                    }

                    if (messageType === "error") {
                        fail(payload.error || "ASR 流错误");
                    }
                };

                socket.onerror = () => {
                    fail("ASR WebSocket 连接错误");
                };

                socket.onclose = () => {
                    if (!handshakeResolved) {
                        fail("ASR WebSocket 连接已关闭");
                        return;
                    }
                    if (asrSocketRef.current === socket) {
                        asrSocketRef.current = null;
                    }
                    streamingActiveRef.current = false;
                };
            } catch (err) {
                reject(err instanceof Error ? err : new Error(String(err)));
            }
        });
    }, [cleanupRecording, closeASRSocket, setChatMessages, setError, setIsRecording, setLiveTranscript, setTranscripts]);

    const startRecording = useCallback(async () => {
        if (pendingStart || isRecording) {
            return;
        }

        setError(null);
        setPendingStart(true);

        try {
            const audioContext = await ensureAudioContext();
            await openASRSocket();
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            mediaStreamRef.current = stream;

            const source = audioContext.createMediaStreamSource(stream);
            const workletNode = workletNodeRef.current;
            if (!workletNode) {
                throw new Error("Audio processing node unavailable");
            }

            await audioContext.resume();

            source.connect(workletNode);
            workletNode.connect(audioContext.destination);
            recordedChunksRef.current = [];
            setIsRecording(true);
        } catch (err) {
            setError(err instanceof Error ? err.message : "无法启动录音");
            cleanupRecording();
            closeASRSocket(1011, "start-failed");
        } finally {
            setPendingStart(false);
        }
    }, [cleanupRecording, closeASRSocket, ensureAudioContext, isRecording, openASRSocket, pendingStart]);

    const stopRecording = useCallback(async () => {
        if (!isRecording) {
            return;
        }

        streamingActiveRef.current = false;
        const socket = asrSocketRef.current;
        if (socket && socket.readyState === WebSocket.OPEN) {
            try {
                socket.send(JSON.stringify({ type: "stop" }));
            } catch (err) {
                closeASRSocket(1011, "stop-error");
            }
        }

        recordedChunksRef.current = [];

        cleanupRecording();
        setIsRecording(false);
    }, [cleanupRecording, closeASRSocket, isRecording]);

    const handleSendTTS = useCallback(async () => {
        const trimmed = ttsText.trim();
        if (trimmed === "") {
            setTtsError("请输入要合成的文本");
            return;
        }

        setTtsError(null);
        setTtsPending(true);

        try {
            const response = await fetch(`${API_BASE}/api/audio/tts`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    text: trimmed,
                    voice_type: selectedVoice,
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

            const buffers = [base64ToUint8Array(audioBase64)];
            const merged = mergeUint8Chunks(buffers);
            const blob = new Blob([merged], { type: "audio/mpeg" });

            if (audioUrl) {
                URL.revokeObjectURL(audioUrl);
            }
            const url = URL.createObjectURL(blob);
            setAudioUrl(url);

            requestAnimationFrame(() => {
                if (audioPlayerRef.current) {
                    audioPlayerRef.current.load();
                    audioPlayerRef.current.play().catch(() => {});
                }
            });

            setChatMessages((prev) => [
                ...prev,
                {
                    id: `assistant-${Date.now()}`,
                    role: "assistant",
                    content: trimmed,
                    audio: { url, duration: data.duration, reqid: data.reqid },
                },
            ]);
        } catch (err) {
            setTtsError(err.message || "TTS 请求失败");
        } finally {
            setTtsPending(false);
        }
    }, [audioUrl, selectedVoice, speechSpeed, ttsText]);

    useEffect(() => () => {
        if (audioUrl) {
            URL.revokeObjectURL(audioUrl);
        }
    }, [audioUrl]);

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
                                {role.name.slice(0, 2)}
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
                    {liveTranscript && (
                        <div className="chat-bubble user live">
                            <div className="bubble-content">
                                <p>{liveTranscript}</p>
                                <span className="bubble-meta">实时转写</span>
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
                            value={ttsText}
                            onChange={(event) => setTtsText(event.target.value)}
                            placeholder="输入文本进行语音合成，或使用上方录音按钮。"
                        />
                        <button type="button" className="primary" onClick={handleSendTTS} disabled={ttsPending}>
                            {ttsPending ? "合成中…" : "发送"}
                        </button>
                    </div>

                    {(error || ttsError) && (
                        <div className="error-block">
                            {error && <p>ASR：{error}</p>}
                            {ttsError && <p>TTS：{ttsError}</p>}
                        </div>
                    )}
                </div>

                <footer className="chat-footer">
                    <audio controls ref={audioPlayerRef}>
                        {audioUrl && <source src={audioUrl} type="audio/mpeg" />}
                        您的浏览器不支持 audio 元素。
                    </audio>
                </footer>
            </div>

            <aside className="chat-settings">
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
