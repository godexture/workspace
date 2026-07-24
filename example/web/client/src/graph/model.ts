import type {
    Catalog,
    ConversionSpec,
    FilterEntry,
    FilterSpec,
    PluginEntry,
    PluginSpec,
    Preset,
} from "../api/types";
import type {
    ConversionInputs,
    InputSource,
} from "../conversion/backend/types";

export const MAIN_NODE_ID = "source-main";
export const OUTPUT_NODE_ID = "output";
export const GRAPH_STORAGE_KEY = "godec-graph-v1";
export const GRAPH_VERSION = 1;

export type SourceSelection =
    | { kind: "preset"; presetId: string }
    | { kind: "upload"; name: string; size: number; lastModified: number }
    | null;

export interface SourceData {
    kind: "source";
    primary: boolean;
    selection: SourceSelection;
    demuxer?: PluginSpec;
    decoder?: PluginSpec;
}

export interface FilterData {
    kind: "filter";
    descriptor: FilterEntry;
    values: Record<string, string>;
    parameters: Record<string, string>;
}

export interface OutputData {
    kind: "output";
    muxer: string;
    muxerValues: Record<string, string>;
    codec: string;
    encoderName?: string;
    encoderValues: Record<string, string>;
}

export type GraphNode = {
    id: string;
    position: { x: number; y: number };
} & (SourceData | FilterData | OutputData);

export interface GraphEdge {
    id: string;
    source: string;
    sourcePort: string;
    target: string;
    targetPort: string;
}

export interface GraphDocument {
    version: number;
    nodes: GraphNode[];
    edges: GraphEdge[];
}

export interface GraphCompileResult {
    issues: string[];
    spec?: ConversionSpec;
    inputs?: ConversionInputs;
    mainInput?: InputSource;
}

export function createInitialGraph(
    catalog: Catalog,
    mainPreset?: Preset,
): GraphDocument {
    const output = catalog.outputs[0];
    const codec = output?.defaultCodec ?? "";
    return {
        version: GRAPH_VERSION,
        nodes: [
            {
                id: MAIN_NODE_ID,
                kind: "source",
                primary: true,
                selection: mainPreset
                    ? { kind: "preset", presetId: mainPreset.id }
                    : null,
                position: { x: 60, y: 40 },
            },
            {
                id: OUTPUT_NODE_ID,
                kind: "output",
                muxer: output?.muxer ?? "",
                muxerValues: {},
                codec,
                encoderName: encoderForCodec(catalog, codec)?.name,
                encoderValues: {},
                position: { x: 60, y: 260 },
            },
        ],
        edges: [
            {
                id: edgeID(MAIN_NODE_ID, "out", OUTPUT_NODE_ID, "in"),
                source: MAIN_NODE_ID,
                sourcePort: "out",
                target: OUTPUT_NODE_ID,
                targetPort: "in",
            },
        ],
    };
}

export function encoderForCodec(
    catalog: Catalog,
    codec: string,
    encoderName?: string,
): PluginEntry | undefined {
    return catalog.encoders.find(
        (entry) => entry.name === (encoderName ?? codec),
    );
}

export function createSourceNode(position: {
    x: number;
    y: number;
}): GraphNode {
    return {
        id: nextNodeID(),
        kind: "source",
        primary: false,
        selection: null,
        position,
    };
}

export function createFilterNode(
    descriptor: FilterEntry,
    position: { x: number; y: number },
): GraphNode {
    return {
        id: nextNodeID(),
        kind: "filter",
        descriptor,
        values: {},
        parameters: {},
        position,
    };
}

export function edgeID(
    source: string,
    sourcePort: string,
    target: string,
    targetPort: string,
): string {
    return `${source}:${sourcePort}->${target}:${targetPort}`;
}

export function inputPorts(node: GraphNode): string[] {
    if (node.kind === "filter") return node.descriptor.inputs;
    if (node.kind === "output") return ["in"];
    return [];
}

export function outputPorts(node: GraphNode): string[] {
    if (node.kind === "source") return ["out"];
    if (node.kind === "filter") return node.descriptor.outputs;
    return [];
}

export function nodeTitle(node: GraphNode): string {
    if (node.kind === "source")
        return node.primary ? "Main audio" : "Audio source";
    if (node.kind === "filter") return displayName(node.descriptor.name);
    return node.muxer ? `${node.muxer.toUpperCase()} output` : "Output";
}

// Identifier used in issue/error text: the catalog's own spelling, not the
// prettified display name (so messages match what's in the catalog).
function nodeIdentifier(node: GraphNode): string {
    return node.kind === "filter" ? node.descriptor.name : nodeTitle(node);
}

// Filter names as shown in the catalog don't always read well capitalized
// override those here rather than guessing from the raw name.
const DISPLAY_NAME_OVERRIDES: Record<string, string> = {
    "remove-dc-offset": "Remove DC Offset",
};

export function displayName(name: string): string {
    return DISPLAY_NAME_OVERRIDES[name] ?? capitalize(name);
}

function capitalize(text: string): string {
    return text
        .split("-")
        .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
        .join(" ");
}

export function canDeleteNode(node: GraphNode): boolean {
    return node.kind === "filter" || (node.kind === "source" && !node.primary);
}

export function compileGraph(
    graph: GraphDocument,
    presets: Preset[],
    files: ReadonlyMap<string, File>,
): GraphCompileResult {
    const issues: string[] = [];
    const byID = new Map(graph.nodes.map((node) => [node.id, node]));
    const main = graph.nodes.find(
        (node): node is GraphNode & SourceData =>
            node.kind === "source" && node.primary,
    );
    const output = graph.nodes.find(
        (node): node is GraphNode & OutputData => node.kind === "output",
    );
    if (!main) issues.push("A main audio source is required.");
    if (!output) issues.push("An output node is required.");

    const incoming = new Map<string, GraphEdge[]>();
    const outgoing = new Map<string, GraphEdge[]>();
    for (const edge of graph.edges) {
        const source = byID.get(edge.source);
        const target = byID.get(edge.target);
        if (!source || !target) {
            issues.push("A connection refers to a removed node.");
            continue;
        }
        if (
            !outputPorts(source).includes(edge.sourcePort) ||
            !inputPorts(target).includes(edge.targetPort)
        ) {
            issues.push("A connection refers to a removed port.");
            continue;
        }
        const sourceKey = `${edge.source}\u0000${edge.sourcePort}`;
        const targetKey = `${edge.target}\u0000${edge.targetPort}`;
        outgoing.set(sourceKey, [...(outgoing.get(sourceKey) ?? []), edge]);
        incoming.set(targetKey, [...(incoming.get(targetKey) ?? []), edge]);
    }

    for (const [key, edges] of outgoing) {
        if (edges.length > 1)
            issues.push(
                `Output ${displayPort(key)} has more than one connection; insert a mixer to branch it.`,
            );
    }
    for (const [key, edges] of incoming) {
        if (edges.length > 1)
            issues.push(
                `Input ${displayPort(key)} has more than one connection.`,
            );
    }
    for (const node of graph.nodes) {
        for (const port of inputPorts(node)) {
            const count = incoming.get(`${node.id}\u0000${port}`)?.length ?? 0;
            if (count === 0)
                issues.push(
                    `${nodeIdentifier(node)}.${port} is not connected.`,
                );
        }
        if (node.kind === "source" && !node.primary) {
            const count = outgoing.get(`${node.id}\u0000out`)?.length ?? 0;
            if (count === 0)
                issues.push(`${nodeIdentifier(node)} is not connected.`);
        }
    }
    if (!main?.selection)
        issues.push("Select an audio file or preset for the main source.");
    if (!output?.muxer || !output.codec)
        issues.push("Select an output format and codec.");

    const filters = graph.nodes.filter(
        (node): node is GraphNode & FilterData => node.kind === "filter",
    );
    const filterOrder = topologicalFilterOrder(filters, graph.edges, byID);
    if (!filterOrder) issues.push("The filter graph contains a cycle.");
    if (issues.length > 0 || !main || !output || !filterOrder)
        return { issues };

    const mainInput = inputSource(main, presets, files, issues);
    const aux: Record<string, InputSource> = {};
    for (const node of graph.nodes) {
        if (node.kind !== "source" || node.primary) continue;
        const source = inputSource(node, presets, files, issues);
        if (!source) {
            issues.push(
                `Select an audio file or preset for ${nodeIdentifier(node)}.`,
            );
            continue;
        }
        aux[node.id] = source;
    }
    if (!mainInput || issues.length > 0) return { issues };

    const specs: FilterSpec[] = filterOrder.map((node) => ({
        name: node.descriptor.name,
        alias: node.id,
        values: nonEmpty(node.values),
        parameters: nonEmpty(node.parameters),
        inputs: Object.fromEntries(
            node.descriptor.inputs.map((port) => {
                const edge = incoming.get(`${node.id}\u0000${port}`)![0];
                return [port, sourceRef(edge.source, edge.sourcePort, byID)];
            }),
        ),
    }));
    const sinkEdge = incoming.get(`${output.id}\u0000in`)![0];
    const spec: ConversionSpec = {
        demuxer: main.demuxer,
        decoder: main.decoder,
        filters: specs,
        auxInputs: Object.fromEntries(
            graph.nodes
                .filter(
                    (node): node is GraphNode & SourceData =>
                        node.kind === "source" && !node.primary,
                )
                .map((node) => [
                    node.id,
                    { demuxer: node.demuxer, decoder: node.decoder },
                ]),
        ),
        sink: sourceRef(sinkEdge.source, sinkEdge.sourcePort, byID),
        muxer: { name: output.muxer, values: nonEmpty(output.muxerValues) },
        codec: output.codec,
        encoder: output.encoderName
            ? {
                  name: output.encoderName,
                  values: nonEmpty(output.encoderValues),
              }
            : undefined,
    };
    return { issues, spec, inputs: { main: mainInput, aux }, mainInput };
}

function topologicalFilterOrder(
    filters: (GraphNode & FilterData)[],
    edges: GraphEdge[],
    byID: ReadonlyMap<string, GraphNode>,
): (GraphNode & FilterData)[] | null {
    const filterIDs = new Set(filters.map((node) => node.id));
    const inDegree = new Map(filters.map((node) => [node.id, 0]));
    const dependents = new Map(
        filters.map((node) => [node.id, [] as string[]]),
    );
    for (const edge of edges) {
        if (!filterIDs.has(edge.source) || !filterIDs.has(edge.target))
            continue;
        if (!byID.has(edge.source) || !byID.has(edge.target)) continue;
        inDegree.set(edge.target, (inDegree.get(edge.target) ?? 0) + 1);
        dependents.get(edge.source)?.push(edge.target);
    }
    const ready = filters
        .filter((node) => inDegree.get(node.id) === 0)
        .map((node) => node.id);
    const ordered: (GraphNode & FilterData)[] = [];
    while (ready.length > 0) {
        const id = ready.shift()!;
        const node = byID.get(id);
        if (!node || node.kind !== "filter") continue;
        ordered.push(node);
        for (const dependent of dependents.get(id) ?? []) {
            const degree = (inDegree.get(dependent) ?? 0) - 1;
            inDegree.set(dependent, degree);
            if (degree === 0) ready.push(dependent);
        }
    }
    return ordered.length === filters.length ? ordered : null;
}

function inputSource(
    node: GraphNode & SourceData,
    presets: Preset[],
    files: ReadonlyMap<string, File>,
    issues: string[],
): InputSource | null {
    const selection = node.selection;
    if (!selection) return null;
    if (selection.kind === "preset") {
        const preset = presets.find((value) => value.id === selection.presetId);
        if (!preset) {
            issues.push(
                `${nodeIdentifier(node)} references an unavailable preset.`,
            );
            return null;
        }
        return { kind: "preset", preset };
    }
    const file = files.get(node.id);
    if (!file) {
        issues.push(`Reselect the uploaded file for ${nodeIdentifier(node)}.`);
        return null;
    }
    return { kind: "upload", file };
}

function sourceRef(
    sourceID: string,
    port: string,
    byID: ReadonlyMap<string, GraphNode>,
) {
    const source = byID.get(sourceID);
    return {
        alias: source?.kind === "source" && source.primary ? "@in" : sourceID,
        port,
    };
}

function nonEmpty(
    values: Record<string, string>,
): Record<string, string> | undefined {
    return Object.keys(values).length > 0 ? values : undefined;
}

function displayPort(key: string): string {
    return key.replace("\u0000", ".");
}

function nextNodeID(): string {
    return `node-${crypto.randomUUID()}`;
}
