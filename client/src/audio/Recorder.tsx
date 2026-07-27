import { useEffect, useRef, useState } from "react";

import { Button } from "../ui";
import { recordingFile } from "./recording";

interface RecorderProps {
    disabled: boolean;
    onRecorded: (file: File) => void;
}

type RecordingState = "idle" | "starting" | "recording" | "saving" | "error";

export function Recorder({ disabled, onRecorded }: RecorderProps) {
    const [state, setState] = useState<RecordingState>("idle");
    const [message, setMessage] = useState<string | null>(null);
    const recorder = useRef<MediaRecorder | null>(null);
    const stream = useRef<MediaStream | null>(null);

    useEffect(() => () => stopStream(stream.current), []);

    async function start() {
        if (disabled || state !== "idle") return;
        if (!navigator.mediaDevices?.getUserMedia || !window.MediaRecorder) {
            setState("error");
            setMessage("This browser does not support microphone recording.");
            return;
        }
        setState("starting");
        setMessage(null);
        try {
            const nextStream = await navigator.mediaDevices.getUserMedia({ audio: true });
            const nextRecorder = new MediaRecorder(nextStream);
            const chunks: Blob[] = [];
            stream.current = nextStream;
            recorder.current = nextRecorder;
            nextRecorder.ondataavailable = (event) => {
                if (event.data.size > 0) chunks.push(event.data);
            };
            nextRecorder.onerror = () => {
                stopStream(nextStream);
                recorder.current = null;
                stream.current = null;
                setState("error");
                setMessage("Recording failed. Please try again.");
            };
            nextRecorder.onstop = () => {
                stopStream(nextStream);
                recorder.current = null;
                stream.current = null;
                setState("saving");
                void recordingFile(chunks, recordingName())
                    .then((file) => {
                        onRecorded(file);
                        setState("idle");
                    })
                    .catch(() => {
                        setState("error");
                        setMessage("Could not prepare the recording as a WAV file.");
                    });
            };
            nextRecorder.start();
            setState("recording");
        } catch (error) {
            setState("error");
            setMessage(recordingError(error));
        }
    }

    function stop() {
        if (state === "recording") recorder.current?.stop();
    }

    return (
        <div>
            <Button variant={state === "recording" ? "danger" : "default"} disabled={disabled || state === "starting" || state === "saving"} onClick={state === "recording" ? stop : start}>
                {state === "recording" ? "Stop recording" : state === "saving" ? "Preparing recording…" : state === "starting" ? "Opening microphone…" : "Record from microphone"}
            </Button>
            {state === "recording" && <small>Recording…</small>}
            {message && <small role="alert">{message}</small>}
        </div>
    );
}

function stopStream(stream: MediaStream | null) {
    stream?.getTracks().forEach((track) => track.stop());
}

function recordingName(): string {
    return `recording-${new Date().toISOString().replace(/[:.]/g, "-")}.wav`;
}

function recordingError(error: unknown): string {
    if (error instanceof DOMException && error.name === "NotAllowedError")
        return "Microphone access was not allowed.";
    return "Could not open the microphone. Check your device and permissions.";
}
