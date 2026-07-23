import { GRAPH_STORAGE_KEY, GRAPH_VERSION, type GraphDocument } from "./model";

export function loadGraph(): GraphDocument | null {
    try {
        const raw = window.localStorage.getItem(GRAPH_STORAGE_KEY);
        if (!raw) return null;
        const graph = JSON.parse(raw) as GraphDocument;
        if (graph.version !== GRAPH_VERSION || !Array.isArray(graph.nodes) || !Array.isArray(graph.edges)) {
            return null;
        }
        return graph;
    } catch {
        return null;
    }
}

export function saveGraph(graph: GraphDocument): void {
    window.localStorage.setItem(GRAPH_STORAGE_KEY, JSON.stringify(graph));
}

export function clearGraph(): void {
    window.localStorage.removeItem(GRAPH_STORAGE_KEY);
}
