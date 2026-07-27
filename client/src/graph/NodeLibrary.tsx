import { useMemo, useState, type DragEvent } from "react";

import type { Catalog, FilterEntry } from "../api/types";
import { displayName, filterRole, type FilterRole } from "./model";
import styles from "./NodeLibrary.module.css";

export const NODE_DRAG_MIME = "application/x-godec-node";

export type NodeDragPayload = { kind: "source" } | { kind: "filter"; name: string };

export type LibrarySelection = { kind: "source" } | { kind: "filter"; descriptor: FilterEntry };

const GROUP_ORDER = ["Source", "Dynamics", "Level", "Spectral", "Time", "Spatial", "Cleanup", "Filter", "Utility", "Output"];

type EntryColor = FilterRole | "source" | "output";

interface LibraryEntry {
    key: string;
    label: string;
    description?: string;
    group: string;
    color: EntryColor;
    payload?: NodeDragPayload;
}

function groupLabel(role: FilterRole): string {
    return role.charAt(0).toUpperCase() + role.slice(1);
}

// Grouping/coloring is entirely driven by filterRole (model.ts) -- the
// single shared classification also used for canvas node and edge colors,
// so the library never drifts out of sync with what's on the canvas.
function entries(catalog: Catalog): LibraryEntry[] {
    const filters: LibraryEntry[] = catalog.filters.map((filter) => {
        const role = filterRole(filter);
        return {
            key: filter.name,
            label: displayName(filter.name),
            description: filter.description,
            group: groupLabel(role),
            color: role,
            payload: { kind: "filter", name: filter.name },
        };
    });
    return [
        { key: "source", label: "Audio Source", group: "Source", color: "source", payload: { kind: "source" } },
        ...filters,
        // There is always exactly one output node already on the canvas
        // (compileGraph requires it), so this entry is shown for parity with
        // the design but isn't draggable/clickable.
        { key: "output", label: "Audio Output", group: "Output", color: "output" },
    ];
}

interface NodeLibraryProps {
    catalog: Catalog;
    disabled?: boolean;
    // Only previews the entry (shown in the Inspector) -- dragging onto the
    // canvas is what actually adds the node. See NODE_DRAG_MIME below.
    onPreview: (selection: LibrarySelection) => void;
}

export function NodeLibrary({ catalog, disabled, onPreview }: NodeLibraryProps) {
    const [query, setQuery] = useState("");
    const all = useMemo(() => entries(catalog), [catalog]);
    const filtered = useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return all;
        return all.filter((entry) => entry.label.toLowerCase().includes(q) || entry.description?.toLowerCase().includes(q));
    }, [all, query]);
    const groups = GROUP_ORDER
        .map((group) => {
            const items = filtered.filter((entry) => entry.group === group);
            return { group, color: items[0]?.color, items };
        })
        .filter((group): group is { group: string; color: EntryColor; items: LibraryEntry[] } => group.items.length > 0);

    function select(entry: LibraryEntry) {
        if (disabled || !entry.payload) return;
        const payload = entry.payload;
        if (payload.kind === "source") {
            onPreview({ kind: "source" });
            return;
        }
        const descriptor = catalog.filters.find((filter) => filter.name === payload.name);
        if (descriptor) onPreview({ kind: "filter", descriptor });
    }

    function dragStart(event: DragEvent, entry: LibraryEntry) {
        if (disabled || !entry.payload) {
            event.preventDefault();
            return;
        }
        event.dataTransfer.setData(NODE_DRAG_MIME, JSON.stringify(entry.payload));
        event.dataTransfer.effectAllowed = "move";
    }

    return (
        <aside className={styles.library}>
            <div className={styles.heading}>Node Library</div>
            <input
                className={styles.search}
                placeholder="Search…"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
            />
            <div className={styles.groups}>
                {groups.map((group) => (
                    <div key={group.group}>
                        <div className={`${styles.category} ${styles[`category_${group.color}`]}`}>
                            {group.group}
                        </div>
                        {group.items.map((entry) => (
                            <div
                                key={entry.key}
                                className={entry.payload ? styles.item : `${styles.item} ${styles.itemDisabled}`}
                                draggable={Boolean(entry.payload) && !disabled}
                                onDragStart={(event) => dragStart(event, entry)}
                                onClick={() => select(entry)}
                                title={entry.description}
                            >
                                <span className={`${styles.dot} ${styles[`dot_${entry.color}`]}`} />
                                <span className={styles.label}>{entry.label}</span>
                            </div>
                        ))}
                    </div>
                ))}
            </div>
        </aside>
    );
}
