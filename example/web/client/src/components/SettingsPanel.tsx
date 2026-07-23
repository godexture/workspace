import { useEffect, useState } from "react";
import { useLocalStorage } from "../hooks/useLocalStorage";

import type { Catalog, ConversionSpec, FilterSpec, PluginField } from "../api/types";
import type { BackendMode } from "../conversion/backend/types";
import { FieldInputs } from "./FieldInputs";
import styles from "./SettingsPanel.module.css";

interface SettingsPanelProps {
    catalog: Catalog;
    mode: BackendMode;
    onModeChange: (mode: BackendMode) => void;
    onSpecChange: (spec: ConversionSpec) => void;
}

interface FilterEntry {
    key: string;
    name: string;
    values: Record<string, string>;
}

let entryKeySeed = 0;
function nextEntryKey(): string {
    entryKeySeed += 1;
    return `filter-${entryKeySeed}`;
}

function applyDefaults(
    values: Record<string, string>,
    fields: PluginField[],
): Record<string, string> {
    const result: Record<string, string> = {};
    for (const field of fields) {
        const val = values[field.name];
        result[field.name] = val === "" || val === undefined ? field.default : val;
    }
    return result;
}

export function SettingsPanel({
    catalog,
    mode,
    onModeChange,
    onSpecChange,
}: SettingsPanelProps) {
    const outputs = catalog.outputs;
    const [muxer, setMuxer] = useLocalStorage("godec-muxer", outputs[0]?.muxer ?? "");
    const selectedOutput = outputs.find((o) => o.muxer === muxer) ?? outputs[0];
    const [codec, setCodec] = useLocalStorage("godec-codec", selectedOutput?.defaultCodec ?? "");

    // Encoder fields are only shown when a catalog encoder entry happens to
    // share the codec's name (true for e.g. flac, not guaranteed for
    // multi-codec plugins like pcm) -- the codec still works either way,
    // just without an exposed advanced form in that case.
    const encoderEntry = catalog.encoders.find((e) => e.name === codec);
    const [encoderValues, setEncoderValues] = useLocalStorage<Record<string, string>>(
        "godec-encoder-values",
        {},
    );

    const [advancedFilters, setAdvancedFilters] = useLocalStorage<FilterEntry[]>("godec-advanced-filters", []);
    const [addFilterChoice, setAddFilterChoice] = useLocalStorage(
        "godec-add-filter-choice",
        catalog.filters[0]?.name ?? "",
    );

    useEffect(() => {
        if (!selectedOutput) return;
        if (!selectedOutput.codecs.includes(codec)) {
            setCodec(selectedOutput.defaultCodec);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [muxer]);

    useEffect(() => {
        const filters: FilterSpec[] = [
            ...advancedFilters.map((entry) => {
                const catalogEntry = catalog.filters.find((f) => f.name === entry.name);
                return {
                    name: entry.name,
                    values: catalogEntry ? applyDefaults(entry.values, catalogEntry.fields) : entry.values,
                };
            }),
        ];
        onSpecChange({
            muxer: { name: muxer },
            codec,
            encoder: encoderEntry
                ? { name: encoderEntry.name, values: applyDefaults(encoderValues, encoderEntry.fields) }
                : undefined,
            filters,
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [muxer, codec, encoderValues, advancedFilters]);

    function addAdvancedFilter() {
        const entry = catalog.filters.find((f) => f.name === addFilterChoice);
        if (!entry) return;
        setAdvancedFilters((prev) => [
            ...prev,
            { key: nextEntryKey(), name: entry.name, values: {} },
        ]);
    }

    function moveAdvancedFilter(index: number, direction: -1 | 1) {
        setAdvancedFilters((prev) => {
            const next = [...prev];
            const target = index + direction;
            if (target < 0 || target >= next.length) return prev;
            [next[index], next[target]] = [next[target], next[index]];
            return next;
        });
    }

    function removeAdvancedFilter(key: string) {
        setAdvancedFilters((prev) => prev.filter((entry) => entry.key !== key));
    }

    return (
        <section className={styles.panel}>
            <h2>Configuration</h2>

            <div className={styles.row}>
                <span className={styles.rowLabel}>Backend</span>
                <div className={styles.modeToggle}>
                    <button
                        type="button"
                        className={
                            mode === "server" ? styles.modeActive : styles.mode
                        }
                        onClick={() => onModeChange("server")}
                    >
                        Online (Server)
                    </button>
                    <button
                        type="button"
                        className={
                            mode === "client" ? styles.modeActive : styles.mode
                        }
                        onClick={() => onModeChange("client")}
                    >
                        Offline (WASM)
                    </button>
                </div>
            </div>

            <div className={styles.row}>
                <span className={styles.rowLabel}>Output Format</span>
                <select
                    value={muxer}
                    onChange={(e) => setMuxer(e.target.value)}
                >
                    {outputs.map((output) => (
                        <option key={output.muxer} value={output.muxer}>
                            {output.muxer.toUpperCase()}
                        </option>
                    ))}
                </select>
                {selectedOutput && selectedOutput.codecs.length > 1 && (
                    <select
                        value={codec}
                        onChange={(e) => setCodec(e.target.value)}
                    >
                        {selectedOutput.codecs.map((c) => (
                            <option key={c} value={c}>
                                {c}
                            </option>
                        ))}
                    </select>
                )}
            </div>
            {encoderEntry && encoderEntry.fields.length > 0 && (
                <FieldInputs
                    fields={encoderEntry.fields}
                    values={encoderValues}
                    onChange={(name, value) =>
                        setEncoderValues((prev) => ({ ...prev, [name]: value }))
                    }
                />
            )}

            <h3 className={styles.subheading}>詳細設定 (フィルタ)</h3>
            <div className={styles.addFilter}>
                <select
                    value={addFilterChoice}
                    onChange={(e) => setAddFilterChoice(e.target.value)}
                >
                    {catalog.filters.map((f) => (
                        <option key={f.name} value={f.name}>
                            {f.name} — {f.description}
                        </option>
                    ))}
                </select>
                <button
                    type="button"
                    onClick={addAdvancedFilter}
                    disabled={!addFilterChoice}
                >
                    追加
                </button>
            </div>
            <ol className={styles.advancedList}>
                {advancedFilters.map((entry, index) => {
                    const catalogEntry = catalog.filters.find(
                        (f) => f.name === entry.name,
                    );
                    return (
                        <li key={entry.key} className={styles.advancedItem}>
                            <div className={styles.advancedHeader}>
                                <span className={styles.filterName}>
                                    {entry.name}
                                </span>
                                <div className={styles.advancedActions}>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            moveAdvancedFilter(index, -1)
                                        }
                                        disabled={index === 0}
                                    >
                                        ↑
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            moveAdvancedFilter(index, 1)
                                        }
                                        disabled={
                                            index === advancedFilters.length - 1
                                        }
                                    >
                                        ↓
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() =>
                                            removeAdvancedFilter(entry.key)
                                        }
                                    >
                                        削除
                                    </button>
                                </div>
                            </div>
                            {catalogEntry && (
                                <FieldInputs
                                    fields={catalogEntry.fields}
                                    values={entry.values}
                                    onChange={(name, value) =>
                                        setAdvancedFilters((prev) =>
                                            prev.map((e) =>
                                                e.key === entry.key
                                                    ? {
                                                          ...e,
                                                          values: {
                                                              ...e.values,
                                                              [name]: value,
                                                          },
                                                      }
                                                    : e,
                                            ),
                                        )
                                    }
                                />
                            )}
                        </li>
                    );
                })}
            </ol>
        </section>
    );
}
