import { useState } from "react";

import type { Preset } from "../api/types";
import { AudioPlayer } from "../audio/AudioPlayer";
import type { InputSource } from "../conversion/backend/types";
import { inputLabel } from "../conversion/backend/types";
import styles from "./InputPanel.module.css";

interface InputPanelProps {
    presets: Preset[];
    input: InputSource | null;
    previewSrc: string | null;
    onChange: (input: InputSource) => void;
    maxUploadBytes: number;
}

export function InputPanel({
    presets,
    input,
    previewSrc,
    onChange,
    maxUploadBytes,
}: InputPanelProps) {
    const [dragOver, setDragOver] = useState(false);
    const [uploadError, setUploadError] = useState<string | null>(null);

    function acceptFile(file: File) {
        if (file.size > maxUploadBytes) {
            setUploadError(
                `Uploaded file exceeds the limit (${formatLimit(maxUploadBytes)}): ${formatLimit(file.size)}`,
            );
            return;
        }
        setUploadError(null);
        onChange({ kind: "upload", file });
    }

    return (
        <section className={styles.panel}>
            <h2>Input</h2>

            <div className={styles.presets}>
                {presets.map((preset) => (
                    <button
                        key={preset.id}
                        type="button"
                        className={
                            input?.kind === "preset" &&
                            input.preset.id === preset.id
                                ? styles.presetActive
                                : styles.preset
                        }
                        onClick={() => onChange({ kind: "preset", preset })}
                    >
                        {preset.name}
                    </button>
                ))}
            </div>

            <label
                className={dragOver ? styles.dropzoneActive : styles.dropzone}
                onDragOver={(e) => {
                    e.preventDefault();
                    setDragOver(true);
                }}
                onDragLeave={() => setDragOver(false)}
                onDrop={(e) => {
                    e.preventDefault();
                    setDragOver(false);
                    const file = e.dataTransfer.files[0];
                    if (file) acceptFile(file);
                }}
            >
                <input
                    type="file"
                    accept="audio/wav,audio/x-wav,audio/flac,audio/mpeg,.wav,.flac,.mp3"
                    className={styles.fileInput}
                    onChange={(e) => {
                        const file = e.target.files?.[0];
                        if (file) acceptFile(file);
                    }}
                />
                Drag and drop a file here, or click to select (WAV / FLAC / MP3,
                max {formatLimit(maxUploadBytes)})
            </label>
            {uploadError && <p className={styles.error}>{uploadError}</p>}

            <AudioPlayer
                label={input ? inputLabel(input) : "Input"}
                src={previewSrc}
                sizeBytes={
                    input?.kind === "upload" ? input.file.size : undefined
                }
            />
        </section>
    );
}

function formatLimit(bytes: number): string {
    if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(0)} GiB`;
    return `${(bytes / (1 << 20)).toFixed(0)} MiB`;
}
