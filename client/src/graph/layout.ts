import dagre from "@dagrejs/dagre";

import type { GraphDocument } from "./model";

const NODE_WIDTH = 210;
const NODE_HEIGHT = 92;

export function layoutGraph(graph: GraphDocument): GraphDocument {
    const dagreGraph = new dagre.graphlib.Graph();
    dagreGraph.setDefaultEdgeLabel(() => ({}));
    dagreGraph.setGraph({ rankdir: "LR", ranksep: 110, nodesep: 52, marginx: 32, marginy: 32 });
    for (const node of graph.nodes) {
        dagreGraph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    }
    for (const edge of graph.edges) {
        dagreGraph.setEdge(edge.source, edge.target);
    }
    dagre.layout(dagreGraph);
    return {
        ...graph,
        nodes: graph.nodes.map((node) => {
            const position = dagreGraph.node(node.id);
            return {
                ...node,
                position: { x: position.x - NODE_WIDTH / 2, y: position.y - NODE_HEIGHT / 2 },
            };
        }),
    };
}
