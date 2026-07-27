import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";

import type { PluginField } from "../api/types";
import { isFieldVisible } from "../components/FieldInputs";
import { filterRole, inputPorts, nodeTitle, outputPorts, type FilterRole, type GraphNode } from "./model";
import styles from "./EditorNode.module.css";

export interface EditorNodeData extends Record<string, unknown> {
    node: GraphNode;
    // Set while a library item is being dragged over this node, meaning a
    // drop here will splice-insert (same as inserting it while this node is
    // selected) rather than just placing it at the drop point.
    dropTarget?: boolean;
}

export type EditorFlowNode = Node<EditorNodeData, "editor">;

const FILTER_ROLE_CLASS: Record<FilterRole, string> = {
    dynamics: styles.kindDynamics,
    level: styles.kindLevel,
    spectral: styles.kindSpectral,
    time: styles.kindTime,
    spatial: styles.kindSpatial,
    cleanup: styles.kindCleanup,
    utility: styles.kindUtility,
    filter: styles.kindFilter,
};

function kindClass(node: GraphNode): string {
    if (node.kind === "source") return styles.kindSource;
    if (node.kind === "output") return styles.kindOutput;
    return FILTER_ROLE_CLASS[filterRole(node.descriptor)];
}

export function EditorNode({ data, selected }: NodeProps<EditorFlowNode>) {
    const { node } = data;
    const highlighted = selected || data.dropTarget;
    const className = [styles.node, kindClass(node), highlighted ? styles.selected : ""].join(" ");
    const inputs = inputPorts(node);
    const outputs = outputPorts(node);
    const previews = previewFields(node);
    return (
        <div className={className}>
            <div className={styles.header}>
                <span className={styles.titleGroup}>
                    <span className={styles.title}>{headerTitle(node)}</span>
                    {node.kind === "source" && node.primary && <span className={styles.main}>MAIN</span>}
                </span>
                <span className={styles.gear} aria-hidden="true">⚙</span>
            </div>
            {previews.length > 0 && (
                <div className={styles.previewList}>
                    {previews.map((preview, index) => (
                        <div className={styles.preview} key={`${preview.label}-${index}`}>
                            <span className={styles.previewLabel}>{preview.label}</span>
                            {preview.kind === "bool" ? (
                                <span className={preview.value === "true" ? styles.dotOn : styles.dotOff} />
                            ) : (
                                <span className={styles.previewValue}>{preview.value}</span>
                            )}
                        </div>
                    ))}
                </div>
            )}
            {(inputs.length > 0 || outputs.length > 0) && (
                <div className={styles.ports}>
                    <div className={styles.portColumn}>
                        {inputs.map((port) => (
                            <div className={styles.portIn} key={port}>
                                <Handle type="target" position={Position.Left} id={port} />
                                <span title={port}>{port}</span>
                            </div>
                        ))}
                    </div>
                    <div className={styles.portColumn}>
                        {outputs.map((port) => (
                            <div className={styles.portOut} key={port}>
                                <span title={port}>{port}</span>
                                <Handle type="source" position={Position.Right} id={port} />
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}

// The canvas header always names the node's structural role -- the specific
// preset/file or codec/format lives in the preview row below instead (see
// previewField), matching how a filter's header names the plugin while its
// settings sit below it.
function headerTitle(node: GraphNode): string {
    if (node.kind === "source") return "Audio Source";
    if (node.kind === "output") return "Audio Output";
    return nodeTitle(node);
}

interface FieldPreview {
    label: string;
    value: string;
    kind: "bool" | "text";
}

// A handful of the node's most relevant settings, shown at a glance without
// opening the Inspector. Capped rather than exhaustive -- this canvas stays
// compact, unlike the Resolved Pipeline view.
const MAX_PREVIEW_FIELDS = 3;

function previewFields(node: GraphNode): FieldPreview[] {
    if (node.kind === "source") {
        const items: FieldPreview[] = [];
        if (node.selection) {
            items.push(
                node.selection.kind === "preset"
                    ? { label: "Preset", value: node.selection.presetId, kind: "text" }
                    : { label: "File", value: node.selection.name, kind: "text" },
            );
        }
        if (node.demuxer) items.push({ label: "Demuxer", value: node.demuxer.name, kind: "text" });
        if (node.decoder) items.push({ label: "Decoder", value: node.decoder.name, kind: "text" });
        return items.slice(0, MAX_PREVIEW_FIELDS);
    }
    if (node.kind === "filter") {
        const visible = [
            ...visibleFields(node.descriptor.parameters, node.parameters),
            ...visibleFields(node.descriptor.fields, node.values),
        ];
        return visible.slice(0, MAX_PREVIEW_FIELDS).map(({ field, values }) => ({
            label: field.name,
            value: values[field.name] ?? field.default,
            kind: field.type === "bool" ? "bool" : "text",
        }));
    }
    if (!node.muxer) return [];
    const items: FieldPreview[] = [{ label: "Format", value: node.muxer.toUpperCase(), kind: "text" }];
    if (node.codec) items.push({ label: "Codec", value: node.codec, kind: "text" });
    return items.slice(0, MAX_PREVIEW_FIELDS);
}

function visibleFields(
    fields: PluginField[],
    values: Record<string, string>,
): { field: PluginField; values: Record<string, string> }[] {
    const byName = new Map(fields.map((field) => [field.name, field]));
    return fields.filter((field) => isFieldVisible(field, values, byName)).map((field) => ({ field, values }));
}
