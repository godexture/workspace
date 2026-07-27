import { useState } from "react";

import type { GraphDocument } from "./model";
import {
    loadPipelineLibrary,
    removePipeline,
    savePipeline,
    type SavedPipeline,
} from "./library";
import { Button } from "../ui";
import styles from "./PipelineLibrary.module.css";

interface PipelineLibraryProps {
    graph: GraphDocument;
    disabled: boolean;
    onLoad: (graph: GraphDocument) => void;
}

export function PipelineLibrary({ graph, disabled, onLoad }: PipelineLibraryProps) {
    const [open, setOpen] = useState(false);
    const [name, setName] = useState("");
    const [entries, setEntries] = useState<SavedPipeline[]>(loadPipelineLibrary);

    function save() {
        const trimmed = name.trim();
        if (!trimmed || disabled) return;
        setEntries(savePipeline(trimmed, graph));
        setName("");
    }

    return (
        <div className={styles.library}>
            <Button
                variant={open ? "primary" : "default"}
                disabled={disabled}
                onClick={() => setOpen((current) => !current)}
            >
                Pipelines
            </Button>
            {open && (
                <div className={styles.popover}>
                    <form
                        className={styles.save}
                        onSubmit={(event) => {
                            event.preventDefault();
                            save();
                        }}
                    >
                        <input
                            aria-label="Pipeline name"
                            placeholder="Pipeline name"
                            value={name}
                            onChange={(event) => setName(event.target.value)}
                        />
                        <Button variant="primary" disabled={!name.trim()}>Save</Button>
                    </form>
                    <p className={styles.note}>Uploaded files are not saved. Choose them again after loading.</p>
                    {entries.length === 0 ? (
                        <p className={styles.empty}>No saved pipelines.</p>
                    ) : (
                        <ul className={styles.entries}>
                            {entries.map((entry) => (
                                <li key={entry.name}>
                                    <Button
                                        className={styles.load}
                                        disabled={disabled}
                                        onClick={() => {
                                            onLoad(entry.graph);
                                            setOpen(false);
                                        }}
                                    >
                                        {entry.name}
                                    </Button>
                                    <Button
                                        variant="danger"
                                        className={styles.remove}
                                        disabled={disabled}
                                        aria-label={`Delete ${entry.name}`}
                                        onClick={() => setEntries(removePipeline(entry.name))}
                                    >
                                        Delete
                                    </Button>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
            )}
        </div>
    );
}
