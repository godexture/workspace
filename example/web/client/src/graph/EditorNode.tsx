import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";

import { inputPorts, nodeTitle, outputPorts, type GraphNode } from "./model";
import styles from "./EditorNode.module.css";

export interface EditorNodeData extends Record<string, unknown> {
    node: GraphNode;
}

export type EditorFlowNode = Node<EditorNodeData, "editor">;

export function EditorNode({ data, selected }: NodeProps<EditorFlowNode>) {
    const { node } = data;
    const className = [
        styles.node,
        node.kind === "source" ? styles.source : "",
        node.kind === "filter" ? styles.filter : "",
        node.kind === "output" ? styles.output : "",
        selected ? styles.selected : "",
    ].filter(Boolean).join(" ");
    return (
        <div className={className}>
            <div className={styles.header}>{node.kind === "filter" ? "Filter" : node.kind === "source" ? "Input" : "Result"}</div>
            <strong>{nodeTitle(node)}</strong>
            <div className={styles.detail}>{detail(node)}</div>
            {inputPorts(node).map((port) => (
                <div className={styles.input} key={port}>
                    <Handle type="target" position={Position.Left} id={port} />
                    {port}
                </div>
            ))}
            {outputPorts(node).map((port) => (
                <div className={styles.output} key={port}>
                    {port}
                    <Handle type="source" position={Position.Right} id={port} />
                </div>
            ))}
        </div>
    );
}

function detail(node: GraphNode): string {
    if (node.kind === "source") {
        if (!node.selection) return "Select audio";
        return node.selection.kind === "preset" ? `Preset: ${node.selection.presetId}` : node.selection.name;
    }
    if (node.kind === "filter") return node.descriptor.description;
    return node.codec || "Select format";
}
