import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";
const TARGET_SAMPLE_RATE = 16000;
const WORKLET_URL = `/worklets/pcm-processor.js`;

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

const mergeInt16Chunks = (chunks) => {
    if (!chunks || chunks.length === 0) {
        return null;
    }

    const totalLength = chunks.reduce((acc, chunk) => acc + chunk.length, 0);
    const result = new Int16Array(totalLength);
    let offset = 0;
    chunks.forEach((chunk) => {
        result.set(chunk, offset);
        offset += chunk.length;
    });
    return result;
};

const uint8ToBase64 = (uint8) => {
    let binary = "";
    const chunkSize = 0x8000;
    for (let i = 0; i < uint8.length; i += chunkSize) {
        const sub = uint8.subarray(i, i + chunkSize);
        binary += String.fromCharCode.apply(null, sub);
    }
    return btoa(binary);
};

const pcmToWavBase64 = (pcm, sampleRate = TARGET_SAMPLE_RATE) => {
    const bytesPerSample = 2;
    const blockAlign = bytesPerSample;
    const buffer = new ArrayBuffer(44 + pcm.length * bytesPerSample);
    const view = new DataView(buffer);

    let offset = 0;
    const writeString = (str) => {
        for (let i = 0; i < str.length; i += 1) {
            view.setUint8(offset + i, str.charCodeAt(i));
        }
        offset += str.length;
    };

    writeString("RIFF");
    view.setUint32(offset, 36 + pcm.length * bytesPerSample, true);
    offset += 4;
    writeString("WAVE");
    writeString("fmt ");
    view.setUint32(offset, 16, true);
    offset += 4;
    view.setUint16(offset, 1, true);
    offset += 2;
    view.setUint16(offset, 1, true);
    offset += 2;
    view.setUint32(offset, sampleRate, true);
    offset += 4;
    view.setUint32(offset, sampleRate * blockAlign, true);
    offset += 4;
    view.setUint16(offset, blockAlign, true);
    offset += 2;
    view.setUint16(offset, bytesPerSample * 8, true);
    offset += 2;
    writeString("data");
    view.setUint32(offset, pcm.length * bytesPerSample, true);
    offset += 4;

    new Int16Array(buffer, offset, pcm.length).set(pcm);

    return uint8ToBase64(new Uint8Array(buffer));
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
    const processorRef = useRef(null);
    const recordedChunksRef = useRef([]);

    const [pendingStart, setPendingStart] = useState(false);
    const [isRecording, setIsRecording] = useState(false);
    const [error, setError] = useState(null);
    const [transcripts, setTranscripts] = useState([]);
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

    const cleanupRecording = useCallback(() => {
        if (processorRef.current) {
            processorRef.current.disconnect();
            processorRef.current = null;
        }
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
    }, []);

    useEffect(() => () => cleanupRecording(), [cleanupRecording]);

    const ensureAudioContext = useCallback(async () => {
        if (!audioContextRef.current) {
            audioContextRef.current = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: TARGET_SAMPLE_RATE });
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
                if (data?.type === "PCM") {
                    recordedChunksRef.current.push(new Int16Array(data.payload));
                }
            };
            workletNodeRef.current = workletNode;
        }

        return audioContext;
    }, []);

    const sendASRRequest = useCallback(async (base64Audio) => {
        const response = await fetch(`${API_BASE}/api/audio/asr`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                audio_data: base64Audio,
                audio_format: "wav",
            }),
        });

        const data = await response.json();
        if (!response.ok) {
            throw new Error(data.detail || data.error || "ASR 请求失败");
        }

        const transcriptText = data.text || "";
        const duration = data.duration_ms;

        if (transcriptText) {
            setTranscripts((prev) => [...prev, { text: transcriptText, reqid: data.reqid, duration }]);
            setChatMessages((prev) => [
                ...prev,
                {
                    id: `user-${Date.now()}`,
                    role: "user",
                    content: transcriptText,
                    metadata: { duration: formatDuration(duration) },
                },
            ]);
        }
    }, []);

    const startRecording = useCallback(async () => {
        if (pendingStart || isRecording) {
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
            const processor = audioContext.createScriptProcessor(4096, 1, 1);

            processor.onaudioprocess = (event) => {
                const input = event.inputBuffer.getChannelData(0);
                const resampled = downsampleFloat32(input, audioContext.sampleRate);
                if (resampled) {
                    workletNode.port.postMessage({ type: "PCM", payload: resampled.buffer }, [resampled.buffer]);
                }
            };

            source.connect(processor);
            processor.connect(audioContext.destination);

            processorRef.current = processor;
            recordedChunksRef.current = [];
            setIsRecording(true);
        } catch (err) {
            setError(err.message || "无法启动录音");
            cleanupRecording();
        } finally {
            setPendingStart(false);
        }
    }, [cleanupRecording, ensureAudioContext, isRecording, pendingStart]);

    const stopRecording = useCallback(async () => {
        if (!isRecording) {
            return;
        }

        workletNodeRef.current?.port.postMessage({ type: "STOP" });

        const chunks = recordedChunksRef.current.slice();
        recordedChunksRef.current = [];

        cleanupRecording();
        setIsRecording(false);

        const pcm = mergeInt16Chunks(chunks);
        if (!pcm || pcm.length === 0) {
            setError("没有捕获到音频，请重试");
            return;
        }

        try {
            setError(null);
            const base64Audio = pcmToWavBase64(pcm);
            await sendASRRequest(base64Audio);
        } catch (err) {
            setError(err.message || "ASR 请求失败");
        }
    }, [cleanupRecording, isRecording, sendASRRequest]);

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
