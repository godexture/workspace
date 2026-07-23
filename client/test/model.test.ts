import { expect, test } from "bun:test";

import type { Catalog, FilterEntry, Preset } from "../src/api/types";
import { compileGraph, createInitialGraph, type GraphDocument } from "../src/graph/model";

const preset: Preset = { id: "lpcm", name: "PCM", filename: "lpcm.wav", contentType: "audio/wav" };
const catalog: Catalog = {
    demuxers: [], decoders: [], encoders: [], muxers: [], filters: [],
    outputs: [{ muxer: "wav", extensions: [".wav"], codecs: ["lpcm"], defaultCodec: "lpcm" }],
};

const single: FilterEntry = {
    role: "filter", name: "gain", description: "Gain", fields: [], parameters: [], inputs: ["in"], outputs: ["out"],
};

const splitter: FilterEntry = {
    role: "filter", name: "mixer", description: "Split", fields: [], parameters: [], inputs: ["in0"], outputs: ["out0", "out1"],
};

const joiner: FilterEntry = {
    role: "filter", name: "mixer", description: "Join", fields: [], parameters: [], inputs: ["in0", "in1"], outputs: ["out0"],
};

test("initial source-to-output graph compiles to an explicit sink", () => {
    const result = compileGraph(createInitialGraph(catalog, preset), [preset], new Map());
    expect(result.issues).toEqual([]);
    expect(result.spec?.sink).toEqual({ alias: "@in", port: "out" });
    expect(result.inputs?.main).toEqual({ kind: "preset", preset });
});

test("mixer branches and joins compile in topological order", () => {
    const graph: GraphDocument = {
        version: 1,
        nodes: [
            ...createInitialGraph(catalog, preset).nodes.filter((node) => node.kind !== "output"),
            { id: "split", kind: "filter", descriptor: splitter, values: {}, parameters: { in: "1", out: "2" }, position: { x: 1, y: 1 } },
            { id: "left", kind: "filter", descriptor: single, values: {}, parameters: {}, position: { x: 2, y: 1 } },
            { id: "right", kind: "filter", descriptor: single, values: {}, parameters: {}, position: { x: 2, y: 2 } },
            { id: "join", kind: "filter", descriptor: joiner, values: {}, parameters: { in: "2", out: "1" }, position: { x: 3, y: 1 } },
            createInitialGraph(catalog, preset).nodes.find((node) => node.kind === "output")!,
        ],
        edges: [
            { id: "a", source: "source-main", sourcePort: "out", target: "split", targetPort: "in0" },
            { id: "b", source: "split", sourcePort: "out0", target: "left", targetPort: "in" },
            { id: "c", source: "split", sourcePort: "out1", target: "right", targetPort: "in" },
            { id: "d", source: "left", sourcePort: "out", target: "join", targetPort: "in0" },
            { id: "e", source: "right", sourcePort: "out", target: "join", targetPort: "in1" },
            { id: "f", source: "join", sourcePort: "out0", target: "output", targetPort: "in" },
        ],
    };
    const result = compileGraph(graph, [preset], new Map());
    expect(result.issues).toEqual([]);
    expect(result.spec?.filters?.map((filter) => filter.alias)).toEqual(["split", "left", "right", "join"]);
    expect(result.spec?.sink).toEqual({ alias: "join", port: "out0" });
});

test("direct fan-out is rejected with guidance to use a mixer", () => {
    const base = createInitialGraph(catalog, preset);
    const graph: GraphDocument = {
        ...base,
        nodes: [...base.nodes, { id: "gain", kind: "filter", descriptor: single, values: {}, parameters: {}, position: { x: 2, y: 2 } }],
        edges: [...base.edges, { id: "extra", source: "source-main", sourcePort: "out", target: "gain", targetPort: "in" }],
    };
    expect(compileGraph(graph, [preset], new Map()).issues.join(" ")).toContain("insert a mixer");
});
