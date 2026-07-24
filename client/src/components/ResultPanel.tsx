import { AudioPlayer } from "../audio/AudioPlayer";
import type { InputSource } from "../conversion/backend/types";
import type { JobState } from "../conversion/useConversionJob";
import { Button, Meter, Panel } from "../ui";
import styles from "./ResultPanel.module.css";

interface ResultPanelProps {
    job: JobState;
    input: InputSource | null;
    inputSrc: string | null;
    outputExtension: string;
    canStart: boolean;
    onStart: () => void;
    onCancel: () => void;
}

const PHASE_LABEL: Record<JobState["phase"], string> = {
    idle: "Not Started",
    running: "Converting",
    completed: "Completed",
    failed: "Failed",
    canceled: "Canceled",
};

export function ResultPanel({
    job,
    input,
    inputSrc,
    outputExtension,
    canStart,
    onStart,
    onCancel,
}: ResultPanelProps) {
    const progress = job.progress;

    return (
        <Panel
            title="Conversion"
            actions={<span className={statusClass(job.phase, styles)}>{PHASE_LABEL[job.phase]}</span>}
        >
            <div className={styles.controls}>
                <Button
                    variant="primary"
                    onClick={onStart}
                    disabled={!canStart || job.phase === "running"}
                >
                    Convert
                </Button>
                <Button onClick={onCancel} disabled={job.phase !== "running"}>
                    Cancel
                </Button>
            </div>

            {progress && (
                <div className={styles.progress}>
                    <Meter percent={progress.percent} />
                    <div className={styles.progressStats}>
                        {progress.percent >= 0 ? (
                            <span>{progress.percent.toFixed(1)}%</span>
                        ) : (
                            <span>{progress.processedItems} items</span>
                        )}
                        <span>Elapsed {formatMs(progress.elapsedMs)}</span>
                        {progress.percent >= 0 && progress.percent < 100 && (
                            <span>
                                Remaining {formatMs(progress.etaMs)} (estimated)
                            </span>
                        )}
                        {progress.speedRatio > 0 && (
                            <span>{progress.speedRatio.toFixed(1)}x</span>
                        )}
                    </div>
                </div>
            )}

            {job.error && <p className={styles.error}>{job.error}</p>}

            <div className={styles.comparison}>
                <AudioPlayer label="Input" src={inputSrc} />
                <AudioPlayer
                    label="Output"
                    src={job.resultUrl}
                    downloadName={
                        input ? `converted.${outputExtension}` : undefined
                    }
                />
            </div>
        </Panel>
    );
}

function statusClass(
    phase: JobState["phase"],
    styles: Record<string, string>,
): string {
    return [styles.status, styles[`status_${phase}`]].filter(Boolean).join(" ");
}

function formatMs(ms: number): string {
    const totalSeconds = Math.round(ms / 1000);

    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    const milliseconds = ms % 1000;

    const minutesStr = minutes.toString().padStart(2, "0");
    const secondsStr = seconds.toString().padStart(2, "0");
    const millisecondsStr = milliseconds.toString().padStart(3, "0");

    return `${minutesStr}:${secondsStr}.${millisecondsStr}`;
}
