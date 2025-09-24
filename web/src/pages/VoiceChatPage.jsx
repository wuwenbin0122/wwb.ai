import React, { useCallback, useEffect, useRef, useState } from "react";

const resolveWsUrl = () => {
    const configured = import.meta.env.VITE_WS_URL;
    if (configured && configured.trim() !== "") {
        return configured.trim();
    }
    const origin = window.location.origin.replace(/^http/, "ws");
    return `${origin}/ws/audio`;
};

const base64ToUint8Array = (base64) => {
    if (!base64) return new Uint8Array();
    const binary = atob(base64);
    const length = binary.length;
    const bytes = new Uint8Array(length);
    for (let i = 0; i < length; i += 1) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
};

const VoiceChatPage = () => {
    const [connectionState, setConnectionState] = useState("disconnected");
    const [isRecording, setIsRecording] = useState(false);
    const [error, setError] = useState(null);
    const [partialTranscript, setPartialTranscript] = useState("");
    const [finalTranscripts, setFinalTranscripts] = useState([]);
    const [audioUrl, setAudioUrl] = useState(null);

    const wsRef = useRef(null);
    const recorderRef = useRef(null);
    const mediaStreamRef = useRef(null);
    const ttsBuffersRef = useRef([]);
    const audioPlayerRef = useRef(null);

    const cleanupMedia = useCallback(() => {
        if (recorderRef.current && recorderRef.current.state !== "inactive") {
            recorderRef.current.stop();
        }
        recorderRef.current = null;

        if (mediaStreamRef.current) {
            mediaStreamRef.current.getTracks().forEach((track) => track.stop());
        }
        mediaStreamRef.current = null;
    }, []);

    const closeSocket = useCallback(() => {
        if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
            try {
                wsRef.current.close();
            } catch (err) {
                console.warn("Failed to close websocket", err);
            }
        }
        wsRef.current = null;
    }, []);

    const connectSocket = useCallback(() => {
        if (wsRef.current && wsRef.current.readyState !== WebSocket.CLOSED && wsRef.current.readyState !== WebSocket.CLOSING) {
            return;
        }

        const wsUrl = resolveWsUrl();
        setConnectionState("connecting");
        const socket = new WebSocket(wsUrl);
        socket.binaryType = "arraybuffer";

        socket.onopen = () => {
            setConnectionState("connected");
            setError(null);
        };

        socket.onclose = () => {
            setConnectionState("disconnected");
            setIsRecording(false);
        };

        socket.onerror = (event) => {
            console.error("WebSocket error", event);
            setError("WebSocket connection error");
        };

        socket.onmessage = (event) => {
            try {
                const message = typeof event.data === "string" ? JSON.parse(event.data) : null;
                if (!message || typeof message !== "object") {
                    return;
                }

                switch (message.type) {
                    case "asr": {
                        if (message.is_final) {
                            setFinalTranscripts((prev) => [...prev, message.text]);
                            setPartialTranscript("");
                            if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
                                wsRef.current.send(
                                    JSON.stringify({
                                        type: "tts_request",
                                        text: message.text,
                                    })
                                );
                            }
                        } else {
                            setPartialTranscript(message.text);
                        }
                        break;
                    }
                    case "tts": {
                        if (!message.audio) {
                            break;
                        }
                        const chunk = base64ToUint8Array(message.audio);
                        if (chunk.length === 0) {
                            break;
                        }
                        ttsBuffersRef.current.push(chunk);
                        if (message.is_final) {
                            const blob = new Blob(ttsBuffersRef.current, { type: "audio/mpeg" });
                            ttsBuffersRef.current = [];
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
                        }
                        break;
                    }
                    case "error": {
                        setError(message.message || "Processing error");
                        break;
                    }
                    default:
                        break;
                }
            } catch (err) {
                console.error("Failed to parse message", err);
            }
        };

        wsRef.current = socket;
    }, [audioUrl, closeSocket]);

    useEffect(() => {
        connectSocket();
        return () => {
            cleanupMedia();
            closeSocket();
            if (audioUrl) {
                URL.revokeObjectURL(audioUrl);
            }
        };
    }, [connectSocket, cleanupMedia, closeSocket, audioUrl]);

    const sendAudioComplete = useCallback(() => {
        if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
            wsRef.current.send(JSON.stringify({ type: "audio_complete" }));
        }
    }, []);

    const startRecording = async () => {
        try {
            connectSocket();
            if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
                setError("WebSocket not connected");
                return;
            }

            const mediaStream = await navigator.mediaDevices.getUserMedia({ audio: true });
            mediaStreamRef.current = mediaStream;
            const recorder = new MediaRecorder(mediaStream, { mimeType: "audio/webm;codecs=opus" });
            recorderRef.current = recorder;

            recorder.ondataavailable = (event) => {
                if (event.data && event.data.size > 0 && wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
                    wsRef.current.send(event.data);
                }
            };

            recorder.onstop = () => {
                sendAudioComplete();
                setIsRecording(false);
            };

            recorder.start(250);
            setIsRecording(true);
            setError(null);
            setPartialTranscript("");
        } catch (err) {
            console.error("Unable to start recording", err);
            setError("Cannot access microphone");
        }
    };

    const stopRecording = () => {
        if (recorderRef.current && recorderRef.current.state !== "inactive") {
            recorderRef.current.stop();
        }
        setIsRecording(false);
        cleanupMedia();
    };

    return (
        <div style={{ padding: "24px" }}>
            <h1>Voice Chat</h1>
            <p>WebSocket: {connectionState}</p>
            <div style={{ display: "flex", gap: "12px", marginBottom: "16px" }}>
                <button type="button" onClick={startRecording} disabled={isRecording || connectionState !== "connected"}>
                    {isRecording ? "Recording..." : "Start Recording"}
                </button>
                <button type="button" onClick={stopRecording} disabled={!isRecording}>
                    Stop Recording
                </button>
            </div>

            {error && <p style={{ color: "red" }}>{error}</p>}

            <section style={{ marginBottom: "16px" }}>
                <h2>Recognized Text</h2>
                <div>
                    {finalTranscripts.map((text, idx) => (
                        <p key={`${text}-${idx}`}>{text}</p>
                    ))}
                    {partialTranscript && <p style={{ opacity: 0.6 }}>… {partialTranscript}</p>}
                </div>
            </section>

            <section>
                <h2>Synthesized Audio</h2>
                <audio controls ref={audioPlayerRef}>
                    {audioUrl && <source src={audioUrl} type="audio/mpeg" />}
                    Your browser does not support the audio element.
                </audio>
            </section>
        </div>
    );
};

export default VoiceChatPage;
