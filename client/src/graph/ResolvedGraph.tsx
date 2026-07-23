import dagre from "@dagrejs/dagre";
import { Background, Controls, ReactFlow, Handle, Position, type Edge, type Node, type NodeProps, type NodeTypes } from "@xyflow/react";
import { useMemo } from "react";

import type { NodeStatus, PipelineDescription, PipelineNode } from "../api/types";
import styles from "./ResolvedGraph.module.css";

interface ResolvedGraphProps {
    description: PipelineDescription | null;
    liveNodes?: NodeStatus[];
    error?: string | null;
}

interface ResolvedData extends Record<string, unknown> {
    node: PipelineNode;
    status?: NodeStatus;
    inputs: string[];
    outputs: string[];
}

type ResolvedFlowNode = Node<ResolvedData, "resolved">;

const nodeTypes = { resolved: ResolvedNode } as NodeTypes;

export function ResolvedGraph({ description, liveNodes, error }: ResolvedGraphProps) {
    const flow = useMemo(() => createFlow(description, liveNodes), [description, liveNodes]);
    if (error) return <p className={styles.error}>Failed to resolve pipeline: {error}</p>;
    if (!description || description.Nodes.length === 0) return <p className={styles.empty}>Connect and configure the graph to preview the resolved pipeline.</p>;
    return (
        <div className={styles.canvas}>
            <ReactFlow
                nodes={flow.nodes}
                edges={flow.edges}
                nodeTypes={nodeTypes}
                fitView
                nodesDraggable={false}
                nodesConnectable={false}
                elementsSelectable={false}
                proOptions={{ hideAttribution: true }}
            >
                <Background gap={18} size={1} />
                <Controls showInteractive={false} />
            </ReactFlow>
        </div>
    );
}

function createFlow(description: PipelineDescription | null, liveNodes?: NodeStatus[]): { nodes: ResolvedFlowNode[]; edges: Edge[] } {
    if (!description) return { nodes: [], edges: [] };
    const status = new Map((liveNodes ?? []).map((node) => [node.id, node]));
    const inputs = new Map<string, string[]>();
    const outputs = new Map<string, string[]>();
    for (const edge of description.Edges) {
        inputs.set(edge.ToNode, [...(inputs.get(edge.ToNode) ?? []), edge.ToPort]);
        outputs.set(edge.FromNode, [...(outputs.get(edge.FromNode) ?? []), edge.FromPort]);
    }
    const graph = new dagre.graphlib.Graph();
    graph.setDefaultEdgeLabel(() => ({}));
    graph.setGraph({ rankdir: "LR", ranksep: 85, nodesep: 42, marginx: 26, marginy: 26 });
    for (const node of description.Nodes) graph.setNode(node.ID, { width: 190, height: 86 });
    for (const edge of description.Edges) graph.setEdge(edge.FromNode, edge.ToNode);
    dagre.layout(graph);
    const nodes = description.Nodes.map((node) => {
        const position = graph.node(node.ID);
        return {
            id: node.ID,
            type: "resolved",
            position: { x: position.x - 95, y: position.y - 43 },
            data: {
                node,
                status: status.get(node.ID),
                inputs: unique(inputs.get(node.ID) ?? []),
                outputs: unique(outputs.get(node.ID) ?? []),
            },
        } satisfies ResolvedFlowNode;
    });
    const edges = description.Edges.map((edge) => ({
        id: `${edge.FromNode}:${edge.FromPort}->${edge.ToNode}:${edge.ToPort}`,
        source: edge.FromNode,
        sourceHandle: edge.FromPort,
        target: edge.ToNode,
        targetHandle: edge.ToPort,
        label: `${edge.FromPort} → ${edge.ToPort}`,
        animated: edge.ProgressSource,
        className: edge.ProgressSource ? styles.progressEdge : undefined,
    }));
    return { nodes, edges };
}

function ResolvedNode({ data }: NodeProps<ResolvedFlowNode>) {
    const state = data.status?.state;
    return (
        <div className={[styles.node, state ? styles[`state_${state}`] : ""].join(" ")}>
            <div className={styles.role}>
                {data.node.Role}
                {data.node.AutoInserted && <span>Auto</span>}
            </div>
            <strong>{data.node.Plugin}</strong>
            {data.status && data.status.state !== "unobserved" && <div className={styles.status}>{data.status.state}</div>}
            {data.status?.error && <div className={styles.nodeError}>{data.status.error}</div>}
            {data.inputs.map((port) => <div className={styles.input} key={port}><Handle type="target" position={Position.Left} id={port} />{port}</div>)}
            {data.outputs.map((port) => <div className={styles.output} key={port}>{port}<Handle type="source" position={Position.Right} id={port} /></div>)}
        </div>
    );
}

function unique(values: string[]): string[] {
    return [...new Set(values)].sort();
}
