import { useCallback, useEffect, useRef, useState } from "react";

import type { ConversionSpec, Progress } from "../api/types";
import type { ConversionBackend, InputSource } from "./backend/types";

export type JobPhase = "idle" | "running" | "completed" | "failed" | "canceled";

export interface JobState {
    phase: JobPhase;
    jobId: string | null;
    progress: Progress | null;
    error: string | null;
    resultUrl: string | null;
}

const idleState: JobState = { phase: "idle", jobId: null, progress: null, error: null, resultUrl: null };

function errorMessage(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

// useConversionJob drives one conversion job through a ConversionBackend
// (Server or Client -- the same hook works with either) and exposes its
// live progress/result as React state.
export function useConversionJob(backend: ConversionBackend) {
    const [state, setState] = useState<JobState>(idleState);
    const unsubscribe = useRef<(() => void) | null>(null);

    const stopWatching = useCallback(() => {
        unsubscribe.current?.();
        unsubscribe.current = null;
    }, []);

    const reset = useCallback(() => {
        stopWatching();
        setState((prev) => {
            if (prev.resultUrl) URL.revokeObjectURL(prev.resultUrl);
            return idleState;
        });
    }, [stopWatching]);

    const loadResult = useCallback(
        async (jobId: string) => {
            try {
                const blob = await backend.getResult(jobId);
                const url = URL.createObjectURL(blob);
                setState((prev) => ({ ...prev, resultUrl: url }));
            } catch (err) {
                setState((prev) => ({ ...prev, phase: "failed", error: errorMessage(err) }));
            }
        },
        [backend],
    );

    const start = useCallback(
        async (input: InputSource, spec: ConversionSpec) => {
            reset();
            setState({ ...idleState, phase: "running" });
            try {
                const jobId = await backend.start(input, spec);
                setState((prev) => ({ ...prev, jobId }));
                unsubscribe.current = backend.subscribe(jobId, (progress) => {
                    const phase: JobPhase = !progress.status || progress.status === "running" ? "running" : progress.status;
                    setState((prev) => ({ ...prev, progress, phase, error: progress.error || prev.error }));
                    if (phase !== "running") {
                        stopWatching();
                        if (phase === "completed") void loadResult(jobId);
                    }
                });
            } catch (err) {
                setState((prev) => ({ ...prev, phase: "failed", error: errorMessage(err) }));
            }
        },
        [backend, reset, stopWatching, loadResult],
    );

    const cancel = useCallback(async () => {
        if (!state.jobId) return;
        await backend.cancel(state.jobId);
    }, [backend, state.jobId]);

    useEffect(() => stopWatching, [stopWatching]);

    return { state, start, cancel, reset };
}
