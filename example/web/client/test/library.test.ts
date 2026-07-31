import { expect, test } from "bun:test";

import { graphForLibrary } from "../src/graph/library";
import type { GraphDocument } from "../src/graph/model";

test("library graphs retain presets and clear upload selections", () => {
    const graph: GraphDocument = {
        version: 1,
        nodes: [
            {
                id: "main",
                kind: "source",
                primary: true,
                selection: { kind: "upload", name: "input.wav", size: 12, lastModified: 1 },
                position: { x: 0, y: 0 },
            },
            {
                id: "aux",
                kind: "source",
                primary: false,
                selection: { kind: "preset", presetId: "lpcm" },
                position: { x: 1, y: 0 },
            },
        ],
        edges: [],
    };

    const saved = graphForLibrary(graph);

    expect(saved.nodes[0]?.kind === "source" && saved.nodes[0].selection).toBeNull();
    expect(saved.nodes[1]).toEqual(graph.nodes[1]);
    expect(graph.nodes[0]?.kind === "source" && graph.nodes[0].selection?.kind).toBe("upload");
});
