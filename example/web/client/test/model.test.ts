import { expect, test } from "bun:test";

import type { Catalog, FilterEntry, PluginEntry, Preset } from "../src/api/types";
import { layoutGraph } from "../src/graph/layout";
import { compileGraph, createInitialGraph, encoderForCodec, selectMainSource, type GraphDocument } from "../src/graph/model";

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

const flacEncoder: PluginEntry = {
    role: "encoder", name: "flac", description: "FLAC encoder",
    fields: [{ name: "block-size", type: "int", help: "FLAC block size", default: "4096" }],
};

test("initial source-to-output graph compiles to an explicit sink", () => {
    const result = compileGraph(createInitialGraph(catalog, preset), [preset], new Map());
    expect(result.issues).toEqual([]);
    expect(result.spec?.sink).toEqual({ alias: "@in", port: "out" });
    expect(result.inputs?.main).toEqual({ kind: "preset", preset });
});

test("output defaults and persisted graphs resolve the FLAC encoder", () => {
    const flacCatalog: Catalog = {
        ...catalog,
        encoders: [flacEncoder],
        outputs: [{ muxer: "flac", extensions: [".flac"], codecs: ["flac"], defaultCodec: "flac" }],
    };
    const output = createInitialGraph(flacCatalog, preset).nodes.find((node) => node.kind === "output");
    expect(output?.encoderName).toBe("flac");
    expect(encoderForCodec(flacCatalog, "flac")?.fields).toEqual(flacEncoder.fields);
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

test("connected auxiliary sources require an audio selection", () => {
    const base = createInitialGraph(catalog, preset);
    const graph: GraphDocument = {
        ...base,
        nodes: [
            base.nodes.find((node) => node.kind === "source")!,
            { id: "aux", kind: "source", primary: false, selection: null, position: { x: 1, y: 1 } },
            { id: "join", kind: "filter", descriptor: joiner, values: {}, parameters: {}, position: { x: 2, y: 1 } },
            base.nodes.find((node) => node.kind === "output")!,
        ],
        edges: [
            { id: "a", source: "source-main", sourcePort: "out", target: "join", targetPort: "in0" },
            { id: "b", source: "aux", sourcePort: "out", target: "join", targetPort: "in1" },
            { id: "c", source: "join", sourcePort: "out0", target: "output", targetPort: "in" },
        ],
    };
    expect(compileGraph(graph, [preset], new Map()).issues.join(" ")).toContain("Select an audio file or preset for Audio source");
});

test("recorded audio is accepted as an input file", () => {
    const file = new File([new Uint8Array([82, 73, 70, 70])], "recording.wav", { type: "audio/wav" });
    const graph = createInitialGraph(catalog);
    graph.nodes[0] = {
        ...graph.nodes[0]!,
        kind: "source",
        primary: true,
        selection: { kind: "upload", name: file.name, size: file.size, lastModified: file.lastModified, recorded: true },
    };

    const result = compileGraph(graph, [], new Map([["source-main", file]]));
    expect(result.issues).toEqual([]);
    expect(result.inputs?.main).toEqual({ kind: "upload", file });
});

test("an auxiliary audio source can become the main input", () => {
    const base = createInitialGraph(catalog, preset);
    const graph: GraphDocument = {
        ...base,
        nodes: [
            base.nodes[0]!,
            { id: "aux", kind: "source", primary: false, selection: { kind: "preset", presetId: "lpcm" }, position: { x: 1, y: 1 } },
            { id: "join", kind: "filter", descriptor: joiner, values: {}, parameters: {}, position: { x: 2, y: 1 } },
            base.nodes[1]!,
        ],
        edges: [
            { id: "a", source: "source-main", sourcePort: "out", target: "join", targetPort: "in0" },
            { id: "b", source: "aux", sourcePort: "out", target: "join", targetPort: "in1" },
            { id: "c", source: "join", sourcePort: "out0", target: "output", targetPort: "in" },
        ],
    };

    const selected = selectMainSource(graph, "aux");
    expect(selected.nodes.filter((node) => node.kind === "source" && node.primary).map((node) => node.id)).toEqual(["aux"]);
    expect(compileGraph(selected, [preset], new Map()).inputs?.main).toEqual({ kind: "preset", preset });
    expect(compileGraph(selected, [preset], new Map()).issues).toEqual([]);
});

test("profiles compilation and layout for a 100-node graph", () => {
    const base = createInitialGraph(catalog, preset);
    const filters = Array.from({ length: 100 }, (_, index) => ({
        id: `gain-${index}`,
        kind: "filter" as const,
        descriptor: single,
        values: {},
        parameters: {},
        position: { x: index * 10, y: 0 },
    }));
    const graph: GraphDocument = {
        ...base,
        nodes: [base.nodes[0]!, ...filters, base.nodes[1]!],
        edges: [
            { id: "start", source: "source-main", sourcePort: "out", target: filters[0]!.id, targetPort: "in" },
            ...filters.slice(1).map((filter, index) => ({
                id: `edge-${index}`,
                source: filters[index]!.id,
                sourcePort: "out",
                target: filter.id,
                targetPort: "in",
            })),
            { id: "end", source: filters.at(-1)!.id, sourcePort: "out", target: "output", targetPort: "in" },
        ],
    };
    const compileStart = performance.now();
    const compiled = compileGraph(graph, [preset], new Map());
    const compiledMs = performance.now() - compileStart;
    const layoutStart = performance.now();
    const laidOut = layoutGraph(graph);
    const layoutMs = performance.now() - layoutStart;

    expect(compiled.issues).toEqual([]);
    expect(laidOut.nodes).toHaveLength(102);
    console.info(`100-node graph: compile ${compiledMs.toFixed(2)}ms, layout ${layoutMs.toFixed(2)}ms`);
});
