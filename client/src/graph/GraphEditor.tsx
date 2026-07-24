import "@xyflow/react/dist/style.css";

import {
    Background,
    Controls,
    MarkerType,
    ReactFlow,
    applyNodeChanges,
    type Connection,
    type Edge,
    type NodeTypes,
    type NodeChange,
} from "@xyflow/react";
import { useEffect, useMemo, useState } from "react";

import type { Catalog, Preset } from "../api/types";
import type {
    BackendMode,
    ConversionBackend,
} from "../conversion/backend/types";
import { Inspector } from "./Inspector";
import { layoutGraph } from "./layout";
import {
    canDeleteNode,
    createFilterNode,
    createSourceNode,
    displayName,
    edgeID,
    inputPorts,
    outputPorts,
    type GraphDocument,
    type GraphEdge,
    type GraphNode,
} from "./model";
import { EditorNode, type EditorFlowNode } from "./EditorNode";
import { Button, Panel, SegmentedControl, Toolbar, ToolbarGroup } from "../ui";
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
    onUndo: () => void;
    onRedo: () => void;
    canUndo: boolean;
    canRedo: boolean;
}

const nodeTypes = { editor: EditorNode } as NodeTypes;

const SOURCE_CHOICE = "source";

function nodeChoices(catalog: Catalog): { value: string; label: string }[] {
    return [
        { value: SOURCE_CHOICE, label: "Source: Audio input" },
        ...catalog.filters.map((filter) => ({
            value: filter.name,
            label: `${displayName(filter.name)}: ${filter.description}`,
        })),
    ];
}

function toFlowNodes(graph: GraphDocument): EditorFlowNode[] {
    return graph.nodes.map(
        (node) =>
            ({
                id: node.id,
                type: "editor",
                position: node.position,
                data: { node },
            }) satisfies EditorFlowNode,
    );
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
    onUndo,
    onRedo,
    canUndo,
    canRedo,
}: GraphEditorProps) {
    const [selectedID, setSelectedID] = useState<string | null>(null);
    const [nodeChoice, setNodeChoice] = useState(SOURCE_CHOICE);
    const [editorError, setEditorError] = useState<string | null>(null);
    const [flowNodes, setFlowNodes] = useState<EditorFlowNode[]>(() =>
        toFlowNodes(graph),
    );
    const selected = graph.nodes.find((node) => node.id === selectedID) ?? null;
    useEffect(() => setFlowNodes(toFlowNodes(graph)), [graph.nodes]);
    const flowEdges = useMemo(
        () =>
            graph.edges.map(
                (edge) =>
                    ({
                        id: edge.id,
                        source: edge.source,
                        sourceHandle: edge.sourcePort,
                        target: edge.target,
                        targetHandle: edge.targetPort,
                        type: "smoothstep",
                        markerEnd: { type: MarkerType.ArrowClosed },
                    }) satisfies Edge,
            ),
        [graph.edges],
    );

    function updateNode(next: GraphNode) {
        if (locked) return;
        const previous = graph.nodes.find((node) => node.id === next.id);
        if (
            previous?.kind === "source" &&
            previous.selection?.kind === "upload" &&
            next.kind === "source" &&
            next.selection?.kind !== "upload"
        ) {
            onFileChange(next.id, null);
        }
        onGraphChange({
            ...graph,
            nodes: graph.nodes.map((node) =>
                node.id === next.id ? next : node,
            ),
        });
    }

    function onNodesChange(changes: NodeChange<EditorFlowNode>[]) {
        if (locked) return;
        const accepted = changes.filter((change) => {
            if (change.type !== "remove") return true;
            const node = graph.nodes.find(
                (current) => current.id === change.id,
            );
            return Boolean(node && canDeleteNode(node));
        });
        setFlowNodes((current) => applyNodeChanges(accepted, current));
        const removals = accepted
            .filter((change) => change.type === "remove")
            .map((change) => change.id);
        const next = deleteNodes(graph, removals);
        if (next !== graph) onGraphChange(next);
    }

    function saveNodePosition(id: string, position: { x: number; y: number }) {
        if (
            locked ||
            !Number.isFinite(position.x) ||
            !Number.isFinite(position.y)
        )
            return;
        const current = graph.nodes.find((node) => node.id === id);
        if (
            !current ||
            (current.position.x === position.x &&
                current.position.y === position.y)
        )
            return;
        onGraphChange({
            ...graph,
            nodes: graph.nodes.map((node) =>
                node.id === id ? { ...node, position } : node,
            ),
        });
    }

    function deleteNodes(
        current: GraphDocument,
        ids: Iterable<string>,
    ): GraphDocument {
        let next = current;
        for (const id of ids) {
            const node = next.nodes.find((value) => value.id === id);
            if (!node || !canDeleteNode(node)) continue;
            if (node.kind === "source") onFileChange(id, null);

            const incoming = next.edges.filter((edge) => edge.target === id);
            const outgoing = next.edges.filter((edge) => edge.source === id);
            const bridge: GraphEdge[] =
                incoming.length === 1 && outgoing.length === 1
                    ? [
                          {
                              id: edgeID(
                                  incoming[0].source,
                                  incoming[0].sourcePort,
                                  outgoing[0].target,
                                  outgoing[0].targetPort,
                              ),
                              source: incoming[0].source,
                              sourcePort: incoming[0].sourcePort,
                              target: outgoing[0].target,
                              targetPort: outgoing[0].targetPort,
                          },
                      ]
                    : [];

            next = {
                ...next,
                nodes: next.nodes.filter((value) => value.id !== id),
                edges: [
                    ...next.edges.filter(
                        (edge) => edge.source !== id && edge.target !== id,
                    ),
                    ...bridge,
                ],
            };
            if (selectedID === id) setSelectedID(null);
        }
        return next;
    }

    function onConnect(connection: Connection) {
        if (locked) return;
        if (
            !connection.source ||
            !connection.sourceHandle ||
            !connection.target ||
            !connection.targetHandle
        )
            return;
        if (
            connection.source === connection.target ||
            createsCycle(graph, connection)
        ) {
            setEditorError("Connections cannot create a cycle.");
            return;
        }
        const source = graph.nodes.find(
            (node) => node.id === connection.source,
        );
        const target = graph.nodes.find(
            (node) => node.id === connection.target,
        );
        if (
            !source ||
            !target ||
            !outputPorts(source).includes(connection.sourceHandle) ||
            !inputPorts(target).includes(connection.targetHandle)
        )
            return;
        const edge: GraphEdge = {
            id: edgeID(
                connection.source,
                connection.sourceHandle,
                connection.target,
                connection.targetHandle,
            ),
            source: connection.source,
            sourcePort: connection.sourceHandle,
            target: connection.target,
            targetPort: connection.targetHandle,
        };
        onGraphChange({
            ...graph,
            edges: [
                ...graph.edges.filter(
                    (current) =>
                        !(
                            current.target === edge.target &&
                            current.targetPort === edge.targetPort
                        ) &&
                        !(
                            current.source === edge.source &&
                            current.sourcePort === edge.sourcePort
                        ),
                ),
                edge,
            ],
        });
        setEditorError(null);
    }

    function insertNode() {
        if (locked) return;
        const descriptor =
            nodeChoice === SOURCE_CHOICE
                ? null
                : catalog.filters.find((filter) => filter.name === nodeChoice);
        if (nodeChoice !== SOURCE_CHOICE && !descriptor) return;

        const target =
            selected ??
            graph.nodes.find((current) => current.kind === "output");
        // A terminal node (no outputs, e.g. the output node) has nothing to insert after, so insert before it instead.
        const after = target ? outputPorts(target).length > 0 : false;
        const anchorPort = target ? (after ? outputPorts(target)[0] : inputPorts(target)[0]) : undefined;
        const spliced =
            target && anchorPort
                ? graph.edges.find((edge) =>
                      after
                          ? edge.source === target.id && edge.sourcePort === anchorPort
                          : edge.target === target.id && edge.targetPort === anchorPort,
                  )
                : undefined;
        const neighbor = spliced
            ? graph.nodes.find((current) => current.id === (after ? spliced.target : spliced.source))
            : undefined;

        const position = target
            ? neighbor
                ? {
                      x: (target.position.x + neighbor.position.x) / 2,
                      y: (target.position.y + neighbor.position.y) / 2,
                  }
                : { x: target.position.x + (after ? 220 : -220), y: target.position.y }
            : { x: 60, y: 40 + graph.nodes.length * 24 };

        const node = descriptor
            ? createFilterNode(descriptor, position)
            : createSourceNode(position);

        let edges = graph.edges;
        if (spliced && target && anchorPort) {
            edges = edges.filter((edge) => edge.id !== spliced.id);
            const inPort = inputPorts(node)[0];
            const outPort = outputPorts(node)[0];
            if (after) {
                if (inPort) edges = [...edges, connectPorts(target.id, anchorPort, node.id, inPort)];
                if (outPort) edges = [...edges, connectPorts(node.id, outPort, spliced.target, spliced.targetPort)];
            } else {
                if (outPort) edges = [...edges, connectPorts(node.id, outPort, target.id, anchorPort)];
                if (inPort) edges = [...edges, connectPorts(spliced.source, spliced.sourcePort, node.id, inPort)];
            }
        }

        onGraphChange({ ...graph, nodes: [...graph.nodes, node], edges });
        setSelectedID(node.id);
    }

    async function changeFilterParameters(
        node: GraphNode,
        parameters: Record<string, string>,
    ) {
        if (locked) return;
        if (node.kind !== "filter") return;
        try {
            const descriptor = await backend.describeFilter(
                node.descriptor.name,
                parameters,
            );
            const validInputs = new Set(descriptor.inputs);
            const validOutputs = new Set(descriptor.outputs);
            onGraphChange({
                ...graph,
                nodes: graph.nodes.map((current) =>
                    current.id === node.id
                        ? { ...node, descriptor, parameters }
                        : current,
                ),
                edges: graph.edges
                    .filter(
                        (edge) =>
                            edge.source !== node.id ||
                            validOutputs.has(edge.sourcePort),
                    )
                    .filter(
                        (edge) =>
                            edge.target !== node.id ||
                            validInputs.has(edge.targetPort),
                    ),
            });
            setEditorError(null);
        } catch (error) {
            setEditorError(
                error instanceof Error ? error.message : String(error),
            );
        }
    }

    function upload(node: GraphNode, file: File) {
        if (locked) return;
        if (node.kind !== "source") return;
        const total =
            [...files.entries()]
                .filter(([id]) => id !== node.id)
                .reduce((sum, [, current]) => sum + current.size, 0) +
            file.size;
        if (total > maxUploadBytes) {
            setEditorError(
                `All uploads must stay within ${formatLimit(maxUploadBytes)}.`,
            );
            return;
        }
        onFileChange(node.id, file);
        updateNode({
            ...node,
            selection: {
                kind: "upload",
                name: file.name,
                size: file.size,
                lastModified: file.lastModified,
            },
        });
        setEditorError(null);
    }

    return (
        <Panel
            title="Pipeline Editor"
            description="Connect explicit ports. Use a mixer to split or join streams."
            actions={
                <SegmentedControl
                    value={mode}
                    disabled={locked}
                    onChange={onModeChange}
                    options={[
                        { value: "server", label: "Online (Server)" },
                        { value: "client", label: "Offline (WASM)" },
                    ]}
                />
            }
        >
            <Toolbar>
                <ToolbarGroup>
                    <select
                        className={styles.filterSelect}
                        disabled={locked}
                        value={nodeChoice}
                        onChange={(event) => setNodeChoice(event.target.value)}
                    >
                        {nodeChoices(catalog).map((choice) => (
                            <option key={choice.value} value={choice.value}>
                                {choice.label}
                            </option>
                        ))}
                    </select>
                    <Button
                        variant="primary"
                        onClick={insertNode}
                        disabled={locked || !nodeChoice}
                    >
                        Insert node
                    </Button>
                </ToolbarGroup>
                <ToolbarGroup>
                    <Button disabled={locked || !canUndo} onClick={onUndo}>
                        Undo
                    </Button>
                    <Button disabled={locked || !canRedo} onClick={onRedo}>
                        Redo
                    </Button>
                    <Button
                        disabled={locked}
                        onClick={() => onGraphChange(layoutGraph(graph))}
                    >
                        Auto layout
                    </Button>
                    <Button disabled={locked} onClick={onReset}>
                        Reset
                    </Button>
                </ToolbarGroup>
            </Toolbar>
            {(editorError || issues.length > 0) && (
                <div className={styles.issues}>
                    {editorError && <p>{editorError}</p>}
                    {issues.map((issue) => (
                        <p key={issue}>{issue}</p>
                    ))}
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
                            const removed = new Set(
                                changes
                                    .filter(
                                        (change) => change.type === "remove",
                                    )
                                    .map((change) => change.id),
                            );
                            if (removed.size > 0)
                                onGraphChange({
                                    ...graph,
                                    edges: graph.edges.filter(
                                        (edge) => !removed.has(edge.id),
                                    ),
                                });
                        }}
                        onConnect={onConnect}
                        onNodeDragStop={(_, node) =>
                            saveNodePosition(node.id, node.position)
                        }
                        onNodeClick={(_, node) => setSelectedID(node.id)}
                        onPaneClick={() => setSelectedID(null)}
                        fitView
                        panOnScroll
                        nodesDraggable={!locked}
                        nodesConnectable={!locked}
                        elementsSelectable={!locked}
                        deleteKeyCode={locked ? null : ["Backspace", "Delete"]}
                        proOptions={{ hideAttribution: true }}
                    >
                        <Background
                            gap={18}
                            size={1}
                            color="var(--color-border)"
                        />
                        <Controls showInteractive={false} />
                    </ReactFlow>
                </div>
                <div
                    className={`${styles.inspector}${locked ? ` ${styles.inspectorLocked}` : ""}`}
                >
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
        </Panel>
    );
}

function connectPorts(source: string, sourcePort: string, target: string, targetPort: string): GraphEdge {
    return { id: edgeID(source, sourcePort, target, targetPort), source, sourcePort, target, targetPort };
}

function createsCycle(graph: GraphDocument, connection: Connection): boolean {
    if (!connection.source || !connection.target) return false;
    const seen = new Set<string>();
    const visit = (nodeID: string): boolean => {
        if (nodeID === connection.source) return true;
        if (seen.has(nodeID)) return false;
        seen.add(nodeID);
        return graph.edges
            .filter((edge) => edge.source === nodeID)
            .some((edge) => visit(edge.target));
    };
    return visit(connection.target);
}

function formatLimit(bytes: number): string {
    return bytes >= 1 << 30
        ? `${(bytes / (1 << 30)).toFixed(0)} GiB`
        : `${(bytes / (1 << 20)).toFixed(0)} MiB`;
}
