import { useEffect, useMemo, useRef, useState } from "react";

import { fetchCatalog, fetchPresets, presetAudioUrl } from "../api/client";
import type { Catalog, PipelineDescription, Preset } from "../api/types";
import { clientBackend } from "../conversion/backend/clientBackend";
import { serverBackend } from "../conversion/backend/serverBackend";
import type { BackendMode, InputSource } from "../conversion/backend/types";
import { useConversionJob } from "../conversion/useConversionJob";
import { GraphEditor } from "../graph/GraphEditor";
import { compileGraph, createInitialGraph, type GraphDocument } from "../graph/model";
import { clearGraph, loadGraph, saveGraph } from "../graph/storage";
import { ResolvedGraph } from "../graph/ResolvedGraph";
import { useHistory } from "../hooks/useHistory";
import { useKeyboardShortcuts, type ShortcutBinding } from "../hooks/useKeyboardShortcuts";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { ResultPanel } from "../components/ResultPanel";
import { Panel } from "../ui";
import styles from "./App.module.css";

const CLIENT_MAX_UPLOAD_BYTES = 100 << 20;
const SERVER_MAX_UPLOAD_BYTES = 1 << 30;
const RESOLVE_DEBOUNCE_MS = 350;

export function App() {
    const [catalog, setCatalog] = useState<Catalog | null>(null);
    const [presets, setPresets] = useState<Preset[]>([]);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [mode, setMode] = useLocalStorage<BackendMode>("godec-backend-mode", "server");
    const history = useHistory<GraphDocument | null>(null);
    const graph = history.value;
    const [files, setFiles] = useState<Map<string, File>>(() => new Map());
    const backend = mode === "server" ? serverBackend : clientBackend;
    const maxUploadBytes = mode === "server" ? SERVER_MAX_UPLOAD_BYTES : CLIENT_MAX_UPLOAD_BYTES;

    useEffect(() => {
        Promise.all([fetchCatalog(), fetchPresets()])
            .then(([nextCatalog, nextPresets]) => {
                setCatalog(nextCatalog);
                setPresets(nextPresets);
                const fallback = nextPresets.find((preset) => preset.id === "lpcm");
                history.reset(loadGraph() ?? createInitialGraph(nextCatalog, fallback));
            })
            .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)));
    }, [history.reset]);

    const compiled = useMemo(
        () => graph ? compileGraph(graph, presets, files) : { issues: ["Loading pipeline editor…"] },
        [graph, presets, files],
    );
    const [resolved, setResolved] = useState<PipelineDescription | null>(null);
    const [resolveError, setResolveError] = useState<string | null>(null);
    const resolveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        if (resolveTimer.current) clearTimeout(resolveTimer.current);
        if (!compiled.spec || !compiled.inputs) {
            setResolved(null);
            setResolveError(null);
            return;
        }
        resolveTimer.current = setTimeout(() => {
            backend.resolvePipeline(compiled.inputs!, compiled.spec!)
                .then((description) => {
                    setResolved(description);
                    setResolveError(null);
                })
                .catch((err: unknown) => {
                    setResolved(null);
                    setResolveError(err instanceof Error ? err.message : String(err));
                });
        }, RESOLVE_DEBOUNCE_MS);
        return () => {
            if (resolveTimer.current) clearTimeout(resolveTimer.current);
        };
    }, [backend, compiled.inputs, compiled.spec]);

    const job = useConversionJob(backend);
    const locked = job.state.phase === "running";
    const [activeJobResolved, setActiveJobResolved] = useState<PipelineDescription | null>(null);
    const mainInput = compiled.mainInput ?? null;
    const [inputSrc, setInputSrc] = useState<string | null>(null);

    const shortcuts = useMemo<ShortcutBinding[]>(() => [
        { key: "z", mod: true, handler: history.undo },
        { key: "z", mod: true, shift: true, handler: history.redo },
        { key: "y", mod: true, handler: history.redo },
    ], [history.undo, history.redo]);
    useKeyboardShortcuts(shortcuts, !locked);

    useEffect(() => {
        if (!mainInput) {
            setInputSrc(null);
            return;
        }
        if (mainInput.kind === "preset") {
            setInputSrc(presetAudioUrl(mainInput.preset.id));
            return;
        }
        const url = URL.createObjectURL(mainInput.file);
        setInputSrc(url);
        return () => URL.revokeObjectURL(url);
    }, [mainInput]);

    const outputExtension = useMemo(() => {
        if (!catalog || !compiled.spec) return "";
        const output = catalog.outputs.find((value) => value.muxer === compiled.spec!.muxer.name);
        return output?.extensions[0]?.replace(/^\./, "") ?? compiled.spec.muxer.name;
    }, [catalog, compiled.spec]);

    function updateGraph(next: GraphDocument) {
        history.set(next);
        saveGraph(next);
    }

    function updateFile(nodeID: string, file: File | null) {
        setFiles((current) => {
            const next = new Map(current);
            if (file) next.set(nodeID, file);
            else next.delete(nodeID);
            return next;
        });
    }

    function resetGraph() {
        if (!catalog) return;
        clearGraph();
        setFiles(new Map());
        updateGraph(createInitialGraph(catalog, presets.find((preset) => preset.id === "lpcm")));
    }

    if (loadError) return <div className={styles.centered}>Unable to connect to server: {loadError}</div>;
    if (!catalog || !graph) return <div className={styles.centered}>Loading...</div>;

    return (
        <div className={styles.app}>
            <header className={styles.header}>
                <h1>GODEC Example — Web Audio Converter</h1>
                <p className={styles.subtitle}>Build audio pipelines visually with Godexture</p>
            </header>

            <main className={styles.main}>
                <GraphEditor
                    graph={graph}
                    files={files}
                    catalog={catalog}
                    presets={presets}
                    backend={backend}
                    mode={mode}
                    maxUploadBytes={maxUploadBytes}
                    issues={compiled.issues}
                    locked={locked}
                    onGraphChange={updateGraph}
                    onFileChange={updateFile}
                    onModeChange={setMode}
                    onReset={resetGraph}
                    onUndo={history.undo}
                    onRedo={history.redo}
                    canUndo={history.canUndo}
                    canRedo={history.canRedo}
                />

                <Panel title="Resolved Pipeline">
                    <ResolvedGraph
                        description={resolved}
                        liveNodes={resolved === activeJobResolved ? job.state.progress?.nodes : undefined}
                        error={resolveError}
                    />
                </Panel>

                <ResultPanel
                    job={job.state}
                    input={mainInput}
                    inputSrc={inputSrc}
                    outputExtension={outputExtension}
                    canStart={Boolean(compiled.inputs && compiled.spec && resolved && !resolveError)}
                    onStart={() => {
                        if (compiled.inputs && compiled.spec && resolved) {
                            setActiveJobResolved(resolved);
                            void job.start(compiled.inputs, compiled.spec);
                        }
                    }}
                    onCancel={() => void job.cancel()}
                />
            </main>
        </div>
    );
}
