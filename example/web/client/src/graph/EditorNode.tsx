import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";

import { inputPorts, nodeTitle, outputPorts, type GraphNode } from "./model";
import styles from "./EditorNode.module.css";

export interface EditorNodeData extends Record<string, unknown> {
    node: GraphNode;
}

export type EditorFlowNode = Node<EditorNodeData, "editor">;

const KIND_CLASS: Record<GraphNode["kind"], string> = {
    source: styles.kindSource,
    filter: styles.kindFilter,
    output: styles.kindOutput,
};

export function EditorNode({ data, selected }: NodeProps<EditorFlowNode>) {
    const { node } = data;
    const className = [styles.node, KIND_CLASS[node.kind], selected ? styles.selected : ""].join(" ");
    const inputs = inputPorts(node);
    const outputs = outputPorts(node);
    return (
        <div className={className}>
            <div className={styles.header}>{nodeTitle(node)}</div>
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
