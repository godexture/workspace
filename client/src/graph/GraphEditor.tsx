import "@xyflow/react/dist/style.css";

import {
    Background,
    Controls,
    ReactFlow,
    applyNodeChanges,
    type Connection,
    type EdgeTypes,
    type NodeTypes,
    type NodeChange,
    type ReactFlowInstance,
} from "@xyflow/react";
import { useEffect, useMemo, useRef, useState, type DragEvent } from "react";

import type { Catalog, FilterEntry, Preset } from "../api/types";
import type {
    BackendMode,
    ConversionBackend,
} from "../conversion/backend/types";
import { FitViewOnReady } from "./FitViewOnReady";
import { GradientEdge, type GradientFlowEdge } from "./GradientEdge";
import { Inspector } from "./Inspector";
import { layoutGraph } from "./layout";
import { PipelineLibrary } from "./PipelineLibrary";
import {
    canDeleteNode,
    createFilterNode,
    createSourceNode,
    duplicateNode,
    edgeID,
    filterRole,
    inputPorts,
    outputPorts,
    roleColorVar,
    selectMainSource,
    type GraphDocument,
    type GraphEdge,
    type GraphNode,
} from "./model";
import { EditorNode, type EditorFlowNode } from "./EditorNode";
import { NODE_DRAG_MIME, NodeLibrary, type LibrarySelection, type NodeDragPayload } from "./NodeLibrary";
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
    onLibraryLoad: (graph: GraphDocument) => void;
}

const nodeTypes = { editor: EditorNode } as NodeTypes;
const edgeTypes = { gradient: GradientEdge } as EdgeTypes;

function kindColorVar(node: GraphNode): string {
    if (node.kind === "source") return "var(--color-source)";
    if (node.kind === "output") return "var(--color-output)";
    return roleColorVar(filterRole(node.descriptor));
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
    onLibraryLoad,
}: GraphEditorProps) {
    const [selectedID, setSelectedID] = useState<string | null>(null);
    const [preview, setPreview] = useState<LibrarySelection | null>(null);
    const [libraryOpen, setLibraryOpen] = useState(false);
    const [dragOverNodeId, setDragOverNodeId] = useState<string | null>(null);
    const [editorError, setEditorError] = useState<string | null>(null);
    const [flowNodes, setFlowNodes] = useState<EditorFlowNode[]>(() =>
        toFlowNodes(graph),
    );
    const reactFlowInstance = useRef<ReactFlowInstance<EditorFlowNode, GradientFlowEdge> | null>(null);
    const selected = graph.nodes.find((node) => node.id === selectedID) ?? null;
    useEffect(() => setFlowNodes(toFlowNodes(graph)), [graph.nodes]);
    const displayNodes = useMemo(
        () =>
            dragOverNodeId
                ? flowNodes.map((node) =>
                      node.id === dragOverNodeId ? { ...node, data: { ...node.data, dropTarget: true } } : node,
                  )
                : flowNodes,
        [flowNodes, dragOverNodeId],
    );
    const flowEdges = useMemo<GradientFlowEdge[]>(
        () =>
            graph.edges.map((edge) => {
                const source = graph.nodes.find((node) => node.id === edge.source);
                const target = graph.nodes.find((node) => node.id === edge.target);
                return {
                    id: edge.id,
                    source: edge.source,
                    sourceHandle: edge.sourcePort,
                    target: edge.target,
                    targetHandle: edge.targetPort,
                    type: "gradient",
                    data: {
                        sourceColor: source ? kindColorVar(source) : "var(--color-muted)",
                        targetColor: target ? kindColorVar(target) : "var(--color-muted)",
                    },
                } satisfies GradientFlowEdge;
            }),
        [graph.edges, graph.nodes],
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

    // explicitTarget lets a drag-drop onto a specific node splice into that
    // node (see onCanvasDrop) instead of the current selection.
    function insertNode(descriptor: FilterEntry | null, explicitTarget?: GraphNode) {
        if (locked) return;

        const target =
            explicitTarget ??
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

    function duplicateSelected(node: GraphNode) {
        if (locked || !canDeleteNode(node)) return;
        const next = duplicateNode(node);
        if (next.kind === "source" && next.selection?.kind === "upload") {
            const file = files.get(node.id);
            if (file) onFileChange(next.id, file);
        }
        onGraphChange({ ...graph, nodes: [...graph.nodes, next] });
        setSelectedID(next.id);
    }

    // Placed exactly where dropped, unlike insertNode's heuristic
    // splice-near-selection placement -- dragging from the library is about
    // choosing where on the canvas the node lands.
    function dropNode(descriptor: FilterEntry | null, position: { x: number; y: number }) {
        if (locked) return;
        const node = descriptor ? createFilterNode(descriptor, position) : createSourceNode(position);
        onGraphChange({ ...graph, nodes: [...graph.nodes, node] });
        setSelectedID(node.id);
    }

    function onCanvasDragOver(event: DragEvent) {
        if (locked || !event.dataTransfer.types.includes(NODE_DRAG_MIME)) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = "move";
        setDragOverNodeId(nodeIdAtPoint(event.clientX, event.clientY));
    }

    function onCanvasDragLeave() {
        setDragOverNodeId(null);
    }

    function onCanvasDrop(event: DragEvent) {
        const raw = event.dataTransfer.getData(NODE_DRAG_MIME);
        const targetId = dragOverNodeId;
        setDragOverNodeId(null);
        if (locked || !raw || !reactFlowInstance.current) return;
        event.preventDefault();
        const payload = JSON.parse(raw) as NodeDragPayload;
        const descriptor = payload.kind === "filter"
            ? catalog.filters.find((filter) => filter.name === payload.name) ?? null
            : null;
        if (payload.kind === "filter" && !descriptor) return;
        // Dropping directly on an existing node splices in like inserting
        // while that node is selected; dropping on empty canvas places the
        // node exactly where it landed with no auto-connection.
        const targetNode = targetId ? graph.nodes.find((node) => node.id === targetId) : undefined;
        if (targetNode) {
            insertNode(descriptor, targetNode);
            return;
        }
        const position = reactFlowInstance.current.screenToFlowPosition({ x: event.clientX, y: event.clientY });
        dropNode(descriptor, position);
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
                    <Button
                        variant={libraryOpen ? "primary" : "default"}
                        onClick={() => {
                            setLibraryOpen((open) => !open);
                            setPreview(null);
                        }}
                    >
                        {libraryOpen ? "Hide Library" : "Show Library"}
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
                <PipelineLibrary graph={graph} disabled={locked} onLoad={onLibraryLoad} />
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
                {libraryOpen && (
                    <NodeLibrary catalog={catalog} disabled={locked} onPreview={setPreview} />
                )}
                <div
                    className={styles.canvas}
                    onDragOver={onCanvasDragOver}
                    onDragLeave={onCanvasDragLeave}
                    onDrop={onCanvasDrop}
                >
                    <ReactFlow
                        nodes={displayNodes}
                        edges={flowEdges}
                        nodeTypes={nodeTypes}
                        edgeTypes={edgeTypes}
                        onInit={(instance) => { reactFlowInstance.current = instance; }}
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
                        onPaneClick={() => {
                            setSelectedID(null);
                            setPreview(null);
                        }}
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
                        <FitViewOnReady once />
                    </ReactFlow>
                </div>
                {(selected || preview) && (
                    <div
                        className={`${styles.inspector}${locked ? ` ${styles.inspectorLocked}` : ""}`}
                    >
                        <Inspector
                            node={selected}
                            preview={preview}
                            catalog={catalog}
                            presets={presets}
                            maxUploadBytes={maxUploadBytes}
                            locked={locked}
                            onChange={updateNode}
                            onUpload={upload}
                            onSelectMainSource={(node) => {
                                if (!locked) onGraphChange(selectMainSource(graph, node.id));
                            }}
                            onFilterParametersChange={changeFilterParameters}
                            onDuplicate={duplicateSelected}
                            onDelete={(node) => {
                                if (locked) return;
                                const next = deleteNodes(graph, [node.id]);
                                if (next !== graph) onGraphChange(next);
                            }}
                        />
                    </div>
                )}
            </div>
        </Panel>
    );
}

function connectPorts(source: string, sourcePort: string, target: string, targetPort: string): GraphEdge {
    return { id: edgeID(source, sourcePort, target, targetPort), source, sourcePort, target, targetPort };
}

function nodeIdAtPoint(x: number, y: number): string | null {
    const hit = document.elementsFromPoint(x, y).find((element) => element.classList.contains("react-flow__node"));
    return hit?.getAttribute("data-id") ?? null;
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
