import { useEffect, useMemo, useRef, useState } from "react";
import { useLocalStorage } from "../hooks/useLocalStorage";

import { fetchCatalog, fetchPresets, presetAudioUrl } from "../api/client";
import type { Catalog, ConversionSpec, Preset } from "../api/types";
import { clientBackend } from "../conversion/backend/clientBackend";
import { serverBackend } from "../conversion/backend/serverBackend";
import type { BackendMode, InputSource } from "../conversion/backend/types";
import { useConversionJob } from "../conversion/useConversionJob";
import { PipelineView } from "../pipeline/PipelineView";
import type { PipelineDescription } from "../api/types";
import { InputPanel } from "../components/InputPanel";
import { ResultPanel } from "../components/ResultPanel";
import { SettingsPanel } from "../components/SettingsPanel";
import styles from "./App.module.css";

const CLIENT_MAX_UPLOAD_BYTES = 100 << 20; // 100 MiB
const SERVER_MAX_UPLOAD_BYTES = 1 << 30; // 1 GiB
const RESOLVE_DEBOUNCE_MS = 350;

export function App() {
    const [catalog, setCatalog] = useState<Catalog | null>(null);
    const [presets, setPresets] = useState<Preset[]>([]);
    const [loadError, setLoadError] = useState<string | null>(null);

    const [mode, setMode] = useLocalStorage<BackendMode>("godec-backend-mode", "server");
    const backend = mode === "server" ? serverBackend : clientBackend;
    const maxUploadBytes =
        mode === "server" ? SERVER_MAX_UPLOAD_BYTES : CLIENT_MAX_UPLOAD_BYTES;

    const [input, setInput] = useState<InputSource | null>(null);
    const [spec, setSpec] = useState<ConversionSpec | null>(null);

    useEffect(() => {
        Promise.all([fetchCatalog(), fetchPresets()])
            .then(([c, p]) => {
                setCatalog(c);
                setPresets(p);
                const pcm = p.find((pre) => pre.id === "lpcm");
                if (pcm) {
                    setInput({ kind: "preset", preset: pcm });
                }
            })
            .catch((err: unknown) =>
                setLoadError(err instanceof Error ? err.message : String(err)),
            );
    }, []);

    const [resolved, setResolved] = useState<PipelineDescription | null>(null);
    const [resolveError, setResolveError] = useState<string | null>(null);
    const resolveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        if (resolveTimer.current) clearTimeout(resolveTimer.current);
        if (!input || !spec) {
            setResolved(null);
            return;
        }
        resolveTimer.current = setTimeout(() => {
            backend
                .resolvePipeline(input, spec)
                .then((description) => {
                    setResolved(description);
                    setResolveError(null);
                })
                .catch((err: unknown) => {
                    setResolved(null);
                    setResolveError(
                        err instanceof Error ? err.message : String(err),
                    );
                });
        }, RESOLVE_DEBOUNCE_MS);
        return () => {
            if (resolveTimer.current) clearTimeout(resolveTimer.current);
        };
    }, [backend, input, spec]);

    const job = useConversionJob(backend);
    const jobReset = job.reset;
    const jobPhase = job.state.phase;

    const [activeJobResolved, setActiveJobResolved] = useState<PipelineDescription | null>(null);

    const [inputSrc, setInputSrc] = useState<string | null>(null);
    useEffect(() => {
        if (!input) {
            setInputSrc(null);
            return;
        }
        if (input.kind === "preset") {
            setInputSrc(presetAudioUrl(input.preset.id));
            return;
        }
        const url = URL.createObjectURL(input.file);
        setInputSrc(url);
        return () => URL.revokeObjectURL(url);
    }, [input]);

    const outputExtension = useMemo(() => {
        if (!catalog || !spec) return "";
        const output = catalog.outputs.find((o) => o.muxer === spec.muxer.name);
        return output?.extensions[0]?.replace(/^\./, "") ?? spec.muxer.name;
    }, [catalog, spec]);

    if (loadError) {
        return (
            <div className={styles.centered}>
                Unable to connect to server: {loadError}
            </div>
        );
    }
    if (!catalog) {
        return <div className={styles.centered}>Loading...</div>;
    }

    return (
        <div className={styles.app}>
            <header className={styles.header}>
                <h1>GODEC Example — Web Audio Converter</h1>
                <p className={styles.subtitle}>Powered by Godexture</p>
            </header>

            <main className={styles.grid}>
                <InputPanel
                    presets={presets}
                    input={input}
                    previewSrc={inputSrc}
                    onChange={setInput}
                    maxUploadBytes={maxUploadBytes}
                />
                <SettingsPanel
                    catalog={catalog}
                    mode={mode}
                    onModeChange={setMode}
                    onSpecChange={setSpec}
                />

                <section className={styles.pipelinePanel}>
                    <h2>Resolved Pipeline</h2>
                    <PipelineView
                        description={resolved}
                        liveNodes={resolved === activeJobResolved ? job.state.progress?.nodes : undefined}
                        error={resolveError}
                    />
                </section>

                <div className={styles.resultWrapper}>
                    <ResultPanel
                        job={job.state}
                        input={input}
                        inputSrc={inputSrc}
                        outputExtension={outputExtension}
                        canStart={Boolean(
                            input && spec && resolved && !resolveError,
                        )}
                        onStart={() => {
                            if (input && spec && resolved) {
                                setActiveJobResolved(resolved);
                                void job.start(input, spec);
                            }
                        }}
                        onCancel={() => void job.cancel()}
                    />
                </div>
            </main>
        </div>
    );
}
