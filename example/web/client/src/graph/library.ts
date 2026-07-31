import { GRAPH_VERSION, type GraphDocument } from "./model";

const LIBRARY_STORAGE_KEY = "godec-pipeline-library-v1";

export interface SavedPipeline {
    name: string;
    graph: GraphDocument;
    updatedAt: number;
}

export function loadPipelineLibrary(): SavedPipeline[] {
    try {
        const raw = window.localStorage.getItem(LIBRARY_STORAGE_KEY);
        if (!raw) return [];
        const entries = JSON.parse(raw) as unknown;
        if (!Array.isArray(entries)) return [];
        return entries
            .filter(isSavedPipeline)
            .sort((left, right) => right.updatedAt - left.updatedAt);
    } catch {
        return [];
    }
}

export function savePipeline(name: string, graph: GraphDocument): SavedPipeline[] {
    const entry: SavedPipeline = {
        name,
        graph: graphForLibrary(graph),
        updatedAt: Date.now(),
    };
    const entries = loadPipelineLibrary().filter((current) => current.name !== name);
    const next = [entry, ...entries];
    window.localStorage.setItem(LIBRARY_STORAGE_KEY, JSON.stringify(next));
    return next;
}

export function removePipeline(name: string): SavedPipeline[] {
    const next = loadPipelineLibrary().filter((entry) => entry.name !== name);
    window.localStorage.setItem(LIBRARY_STORAGE_KEY, JSON.stringify(next));
    return next;
}

export function graphForLibrary(graph: GraphDocument): GraphDocument {
    const copy = JSON.parse(JSON.stringify(graph)) as GraphDocument;
    return {
        ...copy,
        nodes: copy.nodes.map((node) =>
            node.kind === "source" && node.selection?.kind === "upload"
                ? { ...node, selection: null }
                : node,
        ),
    };
}

function isSavedPipeline(value: unknown): value is SavedPipeline {
    if (!value || typeof value !== "object") return false;
    const entry = value as Partial<SavedPipeline>;
    return typeof entry.name === "string" &&
        typeof entry.updatedAt === "number" &&
        isGraphDocument(entry.graph);
}

function isGraphDocument(value: unknown): value is GraphDocument {
    if (!value || typeof value !== "object") return false;
    const graph = value as Partial<GraphDocument>;
    return graph.version === GRAPH_VERSION &&
        Array.isArray(graph.nodes) &&
        Array.isArray(graph.edges);
}
