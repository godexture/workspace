import { useEffect, useRef, useState } from "react";

import styles from "./AudioPlayer.module.css";

interface AudioPlayerProps {
    label: string;
    src: string | null;
    sizeBytes?: number;
    downloadName?: string;
    variant?: "input" | "output";
}

const BAR_COUNT = 96;
const FLAT_BARS: number[] = Array.from({ length: BAR_COUNT }, () => 32);
// Waveform rendering decodes the whole file to PCM up front, so it's
// skipped past these limits (compressed size, then decoded duration) --
// the transport still works as a plain seek bar either way. The byte cap
// mirrors CLIENT_MAX_UPLOAD_BYTES (App.tsx) so it covers everything a
// client-mode upload could produce; uncompressed presets like lpcm run
// well under it too.
const MAX_WAVEFORM_BYTES = 100 << 20;
const MAX_WAVEFORM_DURATION_S = 10 * 60;

export function AudioPlayer({
    label,
    src,
    sizeBytes,
    downloadName,
    variant,
}: AudioPlayerProps) {
    const audioRef = useRef<HTMLAudioElement>(null);
    const [playing, setPlaying] = useState(false);
    const [duration, setDuration] = useState<number | null>(null);
    const [currentTime, setCurrentTime] = useState(0);
    const peaks = useWaveformPeaks(src);

    useEffect(() => {
        setPlaying(false);
        setDuration(null);
        setCurrentTime(0);
    }, [src]);

    function togglePlaying() {
        const audio = audioRef.current;
        if (!audio) return;
        if (audio.paused) void audio.play();
        else audio.pause();
    }

    function seek(value: number) {
        const audio = audioRef.current;
        if (!audio) return;
        audio.currentTime = value;
        setCurrentTime(value);
    }

    const progress = duration ? clamp(currentTime / duration, 0, 1) : 0;
    const playedBars = Math.round(progress * BAR_COUNT);
    const bars = peaks ?? FLAT_BARS;

    const variantClass = variant ? styles[variant] : "";
    return (
        <div className={[styles.player, variantClass].filter(Boolean).join(" ")}>
            <div className={styles.header}>
                <span className={styles.label}>{label}</span>
                {sizeBytes !== undefined && (
                    <span className={styles.size}>
                        {formatBytes(sizeBytes)}
                    </span>
                )}
            </div>
            {src ? (
                <div className={styles.transport}>
                    <button
                        type="button"
                        className={styles.playButton}
                        onClick={togglePlaying}
                        aria-label={playing ? "Pause" : "Play"}
                    >
                        {playing ? "⏸" : "▶"}
                    </button>
                    <div className={styles.wave}>
                        <div className={styles.bars}>
                            {bars.map((height, index) => (
                                <span
                                    key={index}
                                    className={index < playedBars ? styles.barPlayed : styles.barRest}
                                    style={{ height: `${height}%` }}
                                />
                            ))}
                        </div>
                        <div className={styles.playhead} style={{ left: `${progress * 100}%` }} />
                        <input
                            type="range"
                            className={styles.seekInput}
                            min={0}
                            max={duration ?? 0}
                            step={0.01}
                            value={currentTime}
                            disabled={!duration}
                            onChange={(event) => seek(Number(event.target.value))}
                            aria-label="Playback position"
                        />
                    </div>
                    <span className={styles.duration}>
                        {formatDuration(currentTime)} / {duration !== null ? formatDuration(duration) : "--:--"}
                    </span>
                    {downloadName && (
                        <a
                            className={styles.download}
                            href={src}
                            download={downloadName}
                            aria-label="Download"
                            title="Download"
                        >
                            ⬇
                        </a>
                    )}
                    <audio
                        ref={audioRef}
                        className={styles.audio}
                        src={src}
                        onPlay={() => setPlaying(true)}
                        onPause={() => setPlaying(false)}
                        onEnded={() => setPlaying(false)}
                        onLoadedMetadata={(event) => setDuration(event.currentTarget.duration)}
                        onTimeUpdate={(event) => setCurrentTime(event.currentTarget.currentTime)}
                    />
                </div>
            ) : (
                <p className={styles.empty}>Not selected</p>
            )}
        </div>
    );
}

function useWaveformPeaks(src: string | null): number[] | null {
    const [peaks, setPeaks] = useState<number[] | null>(null);

    useEffect(() => {
        setPeaks(null);
        if (!src) return;
        let cancelled = false;
        const controller = new AbortController();

        void (async () => {
            try {
                const response = await fetch(src, { signal: controller.signal });
                const blob = await response.blob();
                if (blob.size > MAX_WAVEFORM_BYTES) return;
                const buffer = await blob.arrayBuffer();
                if (cancelled) return;
                const audioContext = new AudioContext();
                try {
                    const audioBuffer = await audioContext.decodeAudioData(buffer);
                    if (!cancelled && audioBuffer.duration <= MAX_WAVEFORM_DURATION_S) {
                        setPeaks(computePeaks(audioBuffer));
                    }
                } finally {
                    void audioContext.close();
                }
            } catch {
                // Best-effort visualization -- the flat fallback bars still work as a scrubber.
            }
        })();

        return () => {
            cancelled = true;
            controller.abort();
        };
    }, [src]);

    return peaks;
}

function computePeaks(buffer: AudioBuffer): number[] {
    const channels = Array.from({ length: buffer.numberOfChannels }, (_, index) => buffer.getChannelData(index));
    const blockSize = Math.max(1, Math.floor(buffer.length / BAR_COUNT));
    const peaks: number[] = [];
    for (let bar = 0; bar < BAR_COUNT; bar++) {
        const start = bar * blockSize;
        const end = bar === BAR_COUNT - 1 ? buffer.length : start + blockSize;
        let peak = 0;
        for (let i = start; i < end; i++) {
            for (const channel of channels) {
                const value = Math.abs(channel[i]);
                if (value > peak) peak = value;
            }
        }
        peaks.push(peak);
    }
    const max = Math.max(...peaks, 0.0001);
    return peaks.map((value) => 12 + (value / max) * 88);
}

function clamp(value: number, min: number, max: number): number {
    return Math.min(max, Math.max(min, value));
}

function formatDuration(seconds: number): string {
    if (!Number.isFinite(seconds)) return "--:--";
    const total = Math.round(seconds);
    const minutes = Math.floor(total / 60);
    const secs = total % 60;
    return `${minutes}:${secs.toString().padStart(2, "0")}`;
}

function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    const units = ["KiB", "MiB", "GiB"];
    let value = bytes / 1024;
    let unitIndex = 0;
    while (value >= 1024 && unitIndex < units.length - 1) {
        value /= 1024;
        unitIndex += 1;
    }
    return `${value.toFixed(1)} ${units[unitIndex]}`;
}
