import "@xyflow/react/dist/style.css";

import {
    Background,
    Controls,
    ReactFlow,
    applyNodeChanges,
    type Connection,
    type Edge,
    type NodeTypes,
    type NodeChange,
} from "@xyflow/react";
import { useEffect, useMemo, useState } from "react";

import type { Catalog, FilterEntry, Preset } from "../api/types";
import type { BackendMode, ConversionBackend } from "../conversion/backend/types";
import { Inspector } from "./Inspector";
import { layoutGraph } from "./layout";
import {
    canDeleteNode,
    createFilterNode,
    createSourceNode,
    edgeID,
    inputPorts,
    outputPorts,
    type GraphDocument,
    type GraphEdge,
    type GraphNode,
} from "./model";
import { EditorNode, type EditorFlowNode } from "./EditorNode";
import styles from "./GraphEditor.module.css";

interface GraphEditorProps {
    graph: GraphDocument;
    files: ReadonlyMap<string, File>;
    catalog: Catalog;
    presets: Preset[];
    backend: ConversionBackend;
    mode: BackendMode;
    maxUploadBytes: number;
    issues: string[];
    locked: boolean;
    onGraphChange: (graph: GraphDocument) => void;
    onFileChange: (nodeID: string, file: File | null) => void;
    onModeChange: (mode: BackendMode) => void;
    onReset: () => void;
}

const nodeTypes = { editor: EditorNode } as NodeTypes;

function toFlowNodes(graph: GraphDocument): EditorFlowNode[] {
    return graph.nodes.map((node) => ({
        id: node.id,
        type: "editor",
        position: node.position,
        data: { node },
    } satisfies EditorFlowNode));
}

export function GraphEditor({
    graph,
    files,
    catalog,
    presets,
    backend,
    mode,
    maxUploadBytes,
    issues,
    locked,
    onGraphChange,
    onFileChange,
    onModeChange,
    onReset,
}: GraphEditorProps) {
    const [selectedID, setSelectedID] = useState<string | null>(null);
    const [filterChoice, setFilterChoice] = useState(catalog.filters[0]?.name ?? "");
    const [editorError, setEditorError] = useState<string | null>(null);
    const [flowNodes, setFlowNodes] = useState<EditorFlowNode[]>(() => toFlowNodes(graph));
    const selected = graph.nodes.find((node) => node.id === selectedID) ?? null;
    useEffect(() => setFlowNodes(toFlowNodes(graph)), [graph.nodes]);
    const flowEdges = useMemo(() => graph.edges.map((edge) => ({
        id: edge.id,
        source: edge.source,
        sourceHandle: edge.sourcePort,
        target: edge.target,
        targetHandle: edge.targetPort,
        label: `${edge.sourcePort} → ${edge.targetPort}`,
    } satisfies Edge)), [graph.edges]);

    function updateNode(next: GraphNode) {
        if (locked) return;
        const previous = graph.nodes.find((node) => node.id === next.id);
        if (previous?.kind === "source" && previous.selection?.kind === "upload" && next.kind === "source" && next.selection?.kind !== "upload") {
            onFileChange(next.id, null);
        }
        onGraphChange({ ...graph, nodes: graph.nodes.map((node) => node.id === next.id ? next : node) });
    }

    function onNodesChange(changes: NodeChange<EditorFlowNode>[]) {
        if (locked) return;
        const accepted = changes.filter((change) => {
            if (change.type !== "remove") return true;
            const node = graph.nodes.find((current) => current.id === change.id);
            return Boolean(node && canDeleteNode(node));
        });
        setFlowNodes((current) => applyNodeChanges(accepted, current));
        const removals = accepted.filter((change) => change.type === "remove").map((change) => change.id);
        const next = deleteNodes(graph, removals);
        if (next !== graph) onGraphChange(next);
    }

    function saveNodePosition(id: string, position: { x: number; y: number }) {
        if (locked || !Number.isFinite(position.x) || !Number.isFinite(position.y)) return;
        const current = graph.nodes.find((node) => node.id === id);
        if (!current || (current.position.x === position.x && current.position.y === position.y)) return;
        onGraphChange({
            ...graph,
            nodes: graph.nodes.map((node) => node.id === id ? { ...node, position } : node),
        });
    }

    function deleteNodes(current: GraphDocument, ids: Iterable<string>): GraphDocument {
        let next = current;
        for (const id of ids) {
            const node = next.nodes.find((value) => value.id === id);
            if (!node || !canDeleteNode(node)) continue;
            if (node.kind === "source") onFileChange(id, null);
            next = {
                ...next,
                nodes: next.nodes.filter((value) => value.id !== id),
                edges: next.edges.filter((edge) => edge.source !== id && edge.target !== id),
            };
            if (selectedID === id) setSelectedID(null);
        }
        return next;
    }

    function onConnect(connection: Connection) {
        if (locked) return;
        if (!connection.source || !connection.sourceHandle || !connection.target || !connection.targetHandle) return;
        if (connection.source === connection.target || createsCycle(graph, connection)) {
            setEditorError("Connections cannot create a cycle.");
            return;
        }
        const source = graph.nodes.find((node) => node.id === connection.source);
        const target = graph.nodes.find((node) => node.id === connection.target);
        if (!source || !target || !outputPorts(source).includes(connection.sourceHandle) || !inputPorts(target).includes(connection.targetHandle)) return;
        const edge: GraphEdge = {
            id: edgeID(connection.source, connection.sourceHandle, connection.target, connection.targetHandle),
            source: connection.source,
            sourcePort: connection.sourceHandle,
            target: connection.target,
            targetPort: connection.targetHandle,
        };
        onGraphChange({
            ...graph,
            edges: [
                ...graph.edges.filter((current) =>
                    !(current.target === edge.target && current.targetPort === edge.targetPort) &&
                    !(current.source === edge.source && current.sourcePort === edge.sourcePort),
                ),
                edge,
            ],
        });
        setEditorError(null);
    }

    function addSource() {
        if (locked) return;
        const node = createSourceNode({ x: 250 + graph.nodes.length * 20, y: 260 });
        onGraphChange({ ...graph, nodes: [...graph.nodes, node] });
        setSelectedID(node.id);
    }

    function addFilter() {
        if (locked) return;
        const descriptor = catalog.filters.find((filter) => filter.name === filterChoice);
        if (!descriptor) return;
        const node = createFilterNode(descriptor, { x: 300 + graph.nodes.length * 20, y: 120 });
        onGraphChange({ ...graph, nodes: [...graph.nodes, node] });
        setSelectedID(node.id);
    }

    async function changeFilterParameters(node: GraphNode, parameters: Record<string, string>) {
        if (locked) return;
        if (node.kind !== "filter") return;
        try {
            const descriptor = await backend.describeFilter(node.descriptor.name, parameters);
            const validInputs = new Set(descriptor.inputs);
            const validOutputs = new Set(descriptor.outputs);
            onGraphChange({
                ...graph,
                nodes: graph.nodes.map((current) => current.id === node.id
                    ? { ...node, descriptor, parameters }
                    : current),
                edges: graph.edges.filter((edge) =>
                    edge.source !== node.id || validOutputs.has(edge.sourcePort),
                ).filter((edge) =>
                    edge.target !== node.id || validInputs.has(edge.targetPort),
                ),
            });
            setEditorError(null);
        } catch (error) {
            setEditorError(error instanceof Error ? error.message : String(error));
        }
    }

    function upload(node: GraphNode, file: File) {
        if (locked) return;
        if (node.kind !== "source") return;
        const total = [...files.entries()]
            .filter(([id]) => id !== node.id)
            .reduce((sum, [, current]) => sum + current.size, 0) + file.size;
        if (total > maxUploadBytes) {
            setEditorError(`All uploads must stay within ${formatLimit(maxUploadBytes)}.`);
            return;
        }
        onFileChange(node.id, file);
        updateNode({
            ...node,
            selection: { kind: "upload", name: file.name, size: file.size, lastModified: file.lastModified },
        });
        setEditorError(null);
    }

    return (
        <section className={styles.panel}>
            <div className={styles.heading}>
                <div>
                    <h2>Pipeline Editor</h2>
                    <p>Connect explicit ports. Use a mixer to split or join streams.</p>
                </div>
                <div className={styles.modeToggle}>
                    <button type="button" disabled={locked} className={mode === "server" ? styles.modeActive : styles.mode} onClick={() => onModeChange("server")}>Online</button>
                    <button type="button" disabled={locked} className={mode === "client" ? styles.modeActive : styles.mode} onClick={() => onModeChange("client")}>Offline</button>
                </div>
            </div>
            <div className={styles.toolbar}>
                <button type="button" disabled={locked} onClick={addSource}>Add source</button>
                <select disabled={locked} value={filterChoice} onChange={(event) => setFilterChoice(event.target.value)}>
                    {catalog.filters.map((filter) => <option key={filter.name} value={filter.name}>{filter.name} — {filter.description}</option>)}
                </select>
                <button type="button" onClick={addFilter} disabled={locked || !filterChoice}>Add filter</button>
                <button type="button" disabled={locked} onClick={() => onGraphChange(layoutGraph(graph))}>Auto layout</button>
                <button type="button" disabled={locked} onClick={onReset}>Reset</button>
            </div>
            {(editorError || issues.length > 0) && (
                <div className={styles.issues}>
                    {editorError && <p>{editorError}</p>}
                    {issues.map((issue) => <p key={issue}>{issue}</p>)}
                </div>
            )}
            <div className={styles.workspace}>
                <div className={styles.canvas}>
                    <ReactFlow
                        nodes={flowNodes}
                        edges={flowEdges}
                        nodeTypes={nodeTypes}
                        onNodesChange={onNodesChange}
                        onEdgesChange={(changes) => {
                            if (locked) return;
                            const removed = new Set(changes.filter((change) => change.type === "remove").map((change) => change.id));
                            if (removed.size > 0) onGraphChange({ ...graph, edges: graph.edges.filter((edge) => !removed.has(edge.id)) });
                        }}
                        onConnect={onConnect}
                        onNodeDragStop={(_, node) => saveNodePosition(node.id, node.position)}
                        onNodeClick={(_, node) => setSelectedID(node.id)}
                        onPaneClick={() => setSelectedID(null)}
                        fitView
                        nodesDraggable={!locked}
                        nodesConnectable={!locked}
                        elementsSelectable={!locked}
                        deleteKeyCode={locked ? null : ["Backspace", "Delete"]}
                        proOptions={{ hideAttribution: true }}
                    >
                        <Background gap={18} size={1} />
                        <Controls />
                    </ReactFlow>
                </div>
                <div className={locked ? styles.inspectorLocked : undefined}>
                    <Inspector
                        node={selected}
                        catalog={catalog}
                        presets={presets}
                        maxUploadBytes={maxUploadBytes}
                        onChange={updateNode}
                        onUpload={upload}
                        onFilterParametersChange={changeFilterParameters}
                        onDelete={(node) => {
                            if (locked) return;
                            const next = deleteNodes(graph, [node.id]);
                            if (next !== graph) onGraphChange(next);
                        }}
                    />
                </div>
            </div>
        </section>
    );
}

function createsCycle(graph: GraphDocument, connection: Connection): boolean {
    if (!connection.source || !connection.target) return false;
    const seen = new Set<string>();
    const visit = (nodeID: string): boolean => {
        if (nodeID === connection.source) return true;
        if (seen.has(nodeID)) return false;
        seen.add(nodeID);
        return graph.edges.filter((edge) => edge.source === nodeID).some((edge) => visit(edge.target));
    };
    return visit(connection.target);
}

function formatLimit(bytes: number): string {
    return bytes >= 1 << 30 ? `${(bytes / (1 << 30)).toFixed(0)} GiB` : `${(bytes / (1 << 20)).toFixed(0)} MiB`;
}
