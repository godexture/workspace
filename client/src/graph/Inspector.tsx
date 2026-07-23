import type { Catalog, FilterEntry, PluginEntry, Preset } from "../api/types";
import { FieldInputs } from "../components/FieldInputs";
import type { GraphNode, SourceData } from "./model";
import styles from "./Inspector.module.css";

interface InspectorProps {
    node: GraphNode | null;
    catalog: Catalog;
    presets: Preset[];
    maxUploadBytes: number;
    onChange: (node: GraphNode) => void;
    onUpload: (node: GraphNode & SourceData, file: File) => void;
    onFilterParametersChange: (node: GraphNode, parameters: Record<string, string>) => void;
}

export function Inspector({
    node,
    catalog,
    presets,
    maxUploadBytes,
    onChange,
    onUpload,
    onFilterParametersChange,
}: InspectorProps) {
    if (!node) {
        return <aside className={styles.empty}>Select a node to configure it.</aside>;
    }
    if (node.kind === "source") {
        return (
            <aside className={styles.panel}>
                <h3>{node.primary ? "Main audio source" : "Audio source"}</h3>
                <label className={styles.field}>
                    <span>Preset</span>
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
                </label>
                <label className={styles.fileField}>
                    <span>Upload audio</span>
                    <input
                        type="file"
                        accept="audio/wav,audio/x-wav,audio/flac,audio/mpeg,.wav,.flac,.mp3"
                        onChange={(event) => {
                            const file = event.target.files?.[0];
                            if (file) onUpload(node, file);
                        }}
                    />
                    <small>All source files together: {formatLimit(maxUploadBytes)}</small>
                </label>
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
            </aside>
        );
    }
    if (node.kind === "filter") {
        return (
            <aside className={styles.panel}>
                <h3>{node.descriptor.name}</h3>
                <p className={styles.description}>{node.descriptor.description}</p>
                {node.descriptor.parameters.length > 0 && (
                    <section>
                        <h4>Topology</h4>
                        <FieldInputs
                            fields={node.descriptor.parameters}
                            values={node.parameters}
                            onChange={(name, value) => onFilterParametersChange(node, { ...node.parameters, [name]: value })}
                        />
                    </section>
                )}
                <section>
                    <h4>Settings</h4>
                    <FieldInputs
                        fields={node.descriptor.fields}
                        values={node.values}
                        onChange={(name, value) => onChange({ ...node, values: { ...node.values, [name]: value } })}
                    />
                </section>
            </aside>
        );
    }
    const selectedOutput = catalog.outputs.find((output) => output.muxer === node.muxer) ?? catalog.outputs[0];
    const encoder = node.encoderName ? catalog.encoders.find((entry) => entry.name === node.encoderName) : undefined;
    return (
        <aside className={styles.panel}>
            <h3>Output</h3>
            <label className={styles.field}>
                <span>Format</span>
                <select
                    value={node.muxer}
                    onChange={(event) => {
                        const next = catalog.outputs.find((output) => output.muxer === event.target.value);
                        onChange({
                            ...node,
                            muxer: event.target.value,
                            muxerValues: {},
                            codec: next?.defaultCodec ?? "",
                            encoderName: undefined,
                            encoderValues: {},
                        });
                    }}
                >
                    {catalog.outputs.map((output) => <option key={output.muxer} value={output.muxer}>{output.muxer.toUpperCase()}</option>)}
                </select>
            </label>
            {selectedOutput && (
                <label className={styles.field}>
                    <span>Codec</span>
                    <select
                        value={node.codec}
                        onChange={(event) => {
                            const encoderEntry = catalog.encoders.find((entry) => entry.name === event.target.value);
                            onChange({ ...node, codec: event.target.value, encoderName: encoderEntry?.name, encoderValues: {} });
                        }}
                    >
                        {selectedOutput.codecs.map((codec) => <option key={codec} value={codec}>{codec}</option>)}
                    </select>
                </label>
            )}
            <PluginSelector
                label="Encoder"
                entries={catalog.encoders.filter((entry) => entry.name === node.codec || !node.codec)}
                value={encoder ? { name: encoder.name, values: node.encoderValues } : undefined}
                allowAuto={false}
                onChange={(value) => onChange({ ...node, encoderName: value?.name, encoderValues: value?.values ?? {} })}
            />
            <FieldInputs
                fields={catalog.muxers.find((entry) => entry.name === node.muxer)?.fields ?? []}
                values={node.muxerValues}
                onChange={(name, value) => onChange({ ...node, muxerValues: { ...node.muxerValues, [name]: value } })}
            />
            {encoder && <FieldInputs fields={encoder.fields} values={node.encoderValues} onChange={(name, value) => onChange({ ...node, encoderValues: { ...node.encoderValues, [name]: value } })} />}
        </aside>
    );
}

function PluginSelector({
    label,
    entries,
    value,
    allowAuto = true,
    onChange,
}: {
    label: string;
    entries: PluginEntry[];
    value?: { name: string; values?: Record<string, string> };
    allowAuto?: boolean;
    onChange: (value: { name: string; values?: Record<string, string> } | undefined) => void;
}) {
    const selected = value ? entries.find((entry) => entry.name === value.name) : undefined;
    return (
        <section className={styles.plugin}>
            <label className={styles.field}>
                <span>{label}</span>
                <select
                    value={value?.name ?? ""}
                    onChange={(event) => onChange(event.target.value ? { name: event.target.value, values: {} } : undefined)}
                >
                    {allowAuto && <option value="">Auto</option>}
                    {entries.map((entry) => <option key={entry.name} value={entry.name}>{entry.name}</option>)}
                </select>
            </label>
            {selected && <FieldInputs fields={selected.fields} values={value?.values ?? {}} onChange={(name, next) => onChange({ name: selected.name, values: { ...value?.values, [name]: next } })} />}
        </section>
    );
}

function formatLimit(bytes: number): string {
    return bytes >= 1 << 30 ? `${(bytes / (1 << 30)).toFixed(0)} GiB` : `${(bytes / (1 << 20)).toFixed(0)} MiB`;
}
