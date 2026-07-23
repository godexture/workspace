import styles from "./AudioPlayer.module.css";

interface AudioPlayerProps {
    label: string;
    src: string | null;
    sizeBytes?: number;
    downloadName?: string;
}

export function AudioPlayer({
    label,
    src,
    sizeBytes,
    downloadName,
}: AudioPlayerProps) {
    return (
        <div className={styles.player}>
            <div className={styles.header}>
                <span className={styles.label}>{label}</span>
                {sizeBytes !== undefined && (
                    <span className={styles.size}>
                        {formatBytes(sizeBytes)}
                    </span>
                )}
            </div>
            {src ? (
                <audio controls src={src} />
            ) : (
                <p className={styles.empty}>Not selected</p>
            )}
        </div>
    );
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
