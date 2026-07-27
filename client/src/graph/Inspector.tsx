import { useState } from "react";

import type { Catalog, PluginEntry, PluginField, Preset } from "../api/types";
import { Recorder } from "../audio/Recorder";
import { FieldInputs } from "../components/FieldInputs";
import { Button, Field, Tabs } from "../ui";
import { canDeleteNode, displayName, encoderForCodec, nodeTitle, type GraphNode, type SourceData } from "./model";
import type { LibrarySelection } from "./NodeLibrary";
import styles from "./Inspector.module.css";

interface InspectorProps {
    node: GraphNode | null;
    // Set when a library entry was clicked but not yet dragged onto the
    // canvas -- shows its details without creating anything in the graph.
    preview: LibrarySelection | null;
    catalog: Catalog;
    presets: Preset[];
    maxUploadBytes: number;
    locked: boolean;
    onChange: (node: GraphNode) => void;
    onUpload: (node: GraphNode & SourceData, file: File) => void;
    onSelectMainSource: (node: GraphNode & SourceData) => void;
    onFilterParametersChange: (node: GraphNode, parameters: Record<string, string>) => void;
    onDuplicate: (node: GraphNode) => void;
    onDelete: (node: GraphNode) => void;
}

export function Inspector({
    node,
    preview,
    catalog,
    presets,
    maxUploadBytes,
    locked,
    onChange,
    onUpload,
    onSelectMainSource,
    onFilterParametersChange,
    onDuplicate,
    onDelete,
}: InspectorProps) {
    const [filterTab, setFilterTab] = useState<"topology" | "settings">("topology");
    const [outputTab, setOutputTab] = useState<"parameters" | "advanced">("parameters");

    if (!node) {
        if (!preview) return null;
        return <LibraryPreview selection={preview} />;
    }
    if (node.kind === "source") {
        return (
            <aside className={styles.panel}>
                <div className={styles.summary}>
                    <h3>{node.primary ? "Main audio source" : "Audio source"}</h3>
                    <Field label="Name">
                        <input
                            type="text"
                            placeholder={node.primary ? "Main audio" : "Audio source"}
                            value={node.label ?? ""}
                            onChange={(event) => onChange({ ...node, label: event.target.value || undefined })}
                        />
                    </Field>
                </div>
                <section className={styles.settings}>
                    <Field label="Preset">
                        <select
                            value={node.selection?.kind === "preset" ? node.selection.presetId : ""}
                            onChange={(event) => onChange({
                                ...node,
                                selection: event.target.value ? { kind: "preset", presetId: event.target.value } : null,
                            })}
                        >
                            <option value="">Select a preset</option>
                            {presets.map((preset) => <option key={preset.id} value={preset.id}>{preset.name}</option>)}
                        </select>
                    </Field>
                    <Field label="Upload audio">
                        <input
                            type="file"
                            accept="audio/wav,audio/x-wav,audio/flac,audio/mpeg,.wav,.flac,.mp3"
                            onChange={(event) => {
                                const file = event.target.files?.[0];
                                if (file) onUpload(node, file);
                            }}
                        />
                        <small className={styles.hint}>All source files together: {formatLimit(maxUploadBytes)}</small>
                    </Field>
                    <Field label="Record audio">
                        <Recorder disabled={locked} onRecorded={(file) => onUpload(node, file)} />
                    </Field>
                    {node.selection?.kind === "upload" && <p className={styles.hint}>{node.selection.name}</p>}
                    <PluginSelector
                        label="Demuxer"
                        entries={catalog.demuxers}
                        value={node.demuxer}
                        onChange={(demuxer) => onChange({ ...node, demuxer })}
                    />
                    <PluginSelector
                        label="Decoder"
                        entries={catalog.decoders}
                        value={node.decoder}
                        onChange={(decoder) => onChange({ ...node, decoder })}
                    />
                    <NodeActions
                        node={node}
                        onSelectMainSource={onSelectMainSource}
                        onDuplicate={onDuplicate}
                        onDelete={onDelete}
                    />
                </section>
            </aside>
        );
    }
    if (node.kind === "filter") {
        const hasTopology = node.descriptor.parameters.length > 0;
        const hasSettings = node.descriptor.fields.length > 0;
        const showTabs = hasTopology && hasSettings;
        const activeTab = showTabs ? filterTab : hasTopology ? "topology" : "settings";
        return (
            <aside className={styles.panel}>
                <div className={styles.summary}>
                    <h3>{nodeTitle(node)}</h3>
                    <p className={styles.description}>{node.descriptor.description}</p>
                    <Field label="Name">
                        <input
                            type="text"
                            placeholder={nodeTitle(node)}
                            value={node.label ?? ""}
                            onChange={(event) => onChange({ ...node, label: event.target.value || undefined })}
                        />
                    </Field>
                </div>
                <section className={styles.settings}>
                    {showTabs && (
                        <Tabs
                            value={activeTab}
                            onChange={setFilterTab}
                            options={[
                                { value: "topology", label: "Topology" },
                                { value: "settings", label: "Settings" },
                            ]}
                        />
                    )}
                    {hasTopology && activeTab === "topology" && (
                        <FieldInputs
                            fields={node.descriptor.parameters}
                            values={node.parameters}
                            onChange={(name, value) => onFilterParametersChange(node, { ...node.parameters, [name]: value })}
                        />
                    )}
                    {hasSettings && activeTab === "settings" && (
                        <FieldInputs
                            fields={node.descriptor.fields}
                            values={node.values}
                            onChange={(name, value) => onChange({ ...node, values: { ...node.values, [name]: value } })}
                        />
                    )}
                    <NodeActions node={node} onDuplicate={onDuplicate} onDelete={onDelete} />
                </section>
            </aside>
        );
    }
    const selectedOutput = catalog.outputs.find((output) => output.muxer === node.muxer) ?? catalog.outputs[0];
    // Older saved graphs did not persist encoderName. Fall back to the codec
    // so the visible default encoder and its settings always describe the same state.
    const encoder = encoderForCodec(catalog, node.codec, node.encoderName);
    const muxerFields = catalog.muxers.find((entry) => entry.name === node.muxer)?.fields ?? [];
    const hasAdvanced = Boolean(encoder && encoder.fields.length > 0);
    const activeOutputTab = hasAdvanced ? outputTab : "parameters";
    return (
        <aside className={styles.panel}>
            <div className={styles.summary}>
                <h3>Output</h3>
                <Field label="Format">
                    <select
                        value={node.muxer}
                        onChange={(event) => {
                            const next = catalog.outputs.find((output) => output.muxer === event.target.value);
                            const codec = next?.defaultCodec ?? "";
                            onChange({
                                ...node,
                                muxer: event.target.value,
                                muxerValues: {},
                                codec,
                                encoderName: encoderForCodec(catalog, codec)?.name,
                                encoderValues: {},
                            });
                        }}
                    >
                        {catalog.outputs.map((output) => <option key={output.muxer} value={output.muxer}>{output.muxer.toUpperCase()}</option>)}
                    </select>
                </Field>
                {selectedOutput && (
                    <Field label="Codec">
                        <select
                            value={node.codec}
                            onChange={(event) => {
                                const encoderEntry = catalog.encoders.find((entry) => entry.name === event.target.value);
                                onChange({ ...node, codec: event.target.value, encoderName: encoderEntry?.name, encoderValues: {} });
                            }}
                        >
                            {selectedOutput.codecs.map((codec) => <option key={codec} value={codec}>{codec}</option>)}
                        </select>
                    </Field>
                )}
                <PluginSelector
                    label="Encoder"
                    entries={catalog.encoders.filter((entry) => entry.name === node.codec || !node.codec)}
                    value={encoder ? { name: encoder.name, values: node.encoderValues } : undefined}
                    allowAuto={false}
                    showFields={false}
                    onChange={(value) => onChange({ ...node, encoderName: value?.name, encoderValues: value?.values ?? {} })}
                />
            </div>
            <section className={styles.settings}>
                {hasAdvanced && (
                    <Tabs
                        value={activeOutputTab}
                        onChange={setOutputTab}
                        options={[
                            { value: "parameters", label: "Parameters" },
                            { value: "advanced", label: "Advanced" },
                        ]}
                    />
                )}
                {activeOutputTab === "parameters" && (
                    <FieldInputs
                        fields={muxerFields}
                        values={node.muxerValues}
                        onChange={(name, value) => onChange({ ...node, muxerValues: { ...node.muxerValues, [name]: value } })}
                    />
                )}
                {activeOutputTab === "advanced" && encoder && (
                    <FieldInputs fields={encoder.fields} values={node.encoderValues} onChange={(name, value) => onChange({ ...node, encoderName: encoder.name, encoderValues: { ...node.encoderValues, [name]: value } })} />
                )}
            </section>
        </aside>
    );
}

// A library entry the user clicked but hasn't dragged onto the canvas yet.
// Read-only: nothing here is a real node, so there's nothing to edit.
function LibraryPreview({ selection }: { selection: LibrarySelection }) {
    if (selection.kind === "source") {
        return (
            <aside className={styles.panel}>
                <div className={styles.summary}>
                    <h3>Audio Source</h3>
                    <p className={styles.description}>An audio input for the pipeline.</p>
                </div>
                <p className={styles.hint}>Drag onto the canvas to add it.</p>
            </aside>
        );
    }
    const { descriptor } = selection;
    return (
        <aside className={styles.panel}>
            <div className={styles.summary}>
                <h3>{displayName(descriptor.name)}</h3>
                <p className={styles.description}>{descriptor.description}</p>
            </div>
            <section className={styles.settings}>
                {descriptor.parameters.length > 0 && (
                    <section>
                        <h4>Topology</h4>
                        <PreviewFields fields={descriptor.parameters} />
                    </section>
                )}
                {descriptor.fields.length > 0 && (
                    <section>
                        <h4>Settings</h4>
                        <PreviewFields fields={descriptor.fields} />
                    </section>
                )}
                <p className={styles.hint}>Drag onto the canvas to add this node.</p>
            </section>
        </aside>
    );
}

function PreviewFields({ fields }: { fields: PluginField[] }) {
    return (
        <div className={styles.previewFields}>
            {fields.map((field) => (
                <div key={field.name} className={styles.previewField} title={field.help}>
                    <span className={styles.previewFieldName}>{field.name}</span>
                    <span className={styles.previewFieldDefault}>{field.default}</span>
                </div>
            ))}
        </div>
    );
}

function NodeActions({
    node,
    onSelectMainSource,
    onDuplicate,
    onDelete,
}: {
    node: GraphNode;
    onSelectMainSource?: (node: GraphNode & SourceData) => void;
    onDuplicate: (node: GraphNode) => void;
    onDelete: (node: GraphNode) => void;
}) {
    if (!canDeleteNode(node)) return null;
    return (
        <div className={styles.actions}>
            {node.kind === "source" && onSelectMainSource && (
                <Button className={styles.mainSource} onClick={() => onSelectMainSource(node)}>Set as main source</Button>
            )}
            <Button className={styles.duplicate} onClick={() => onDuplicate(node)}>Duplicate</Button>
            <Button variant="danger" className={styles.delete} onClick={() => onDelete(node)}>Delete node</Button>
        </div>
    );
}

function PluginSelector({
    label,
    entries,
    value,
    allowAuto = true,
    showFields = true,
    onChange,
}: {
    label: string;
    entries: PluginEntry[];
    value?: { name: string; values?: Record<string, string> };
    allowAuto?: boolean;
    showFields?: boolean;
    onChange: (value: { name: string; values?: Record<string, string> } | undefined) => void;
}) {
    const selected = value ? entries.find((entry) => entry.name === value.name) : undefined;
    return (
        <section className={styles.plugin}>
            <Field label={label}>
                <select
                    value={value?.name ?? ""}
                    onChange={(event) => onChange(event.target.value ? { name: event.target.value, values: {} } : undefined)}
                >
                    {allowAuto && <option value="">Auto</option>}
                    {entries.map((entry) => <option key={entry.name} value={entry.name}>{entry.name}</option>)}
                </select>
            </Field>
            {showFields && selected && <FieldInputs fields={selected.fields} values={value?.values ?? {}} onChange={(name, next) => onChange({ name: selected.name, values: { ...value?.values, [name]: next } })} />}
        </section>
    );
}

function formatLimit(bytes: number): string {
    return bytes >= 1 << 30 ? `${(bytes / (1 << 30)).toFixed(0)} GiB` : `${(bytes / (1 << 20)).toFixed(0)} MiB`;
}
