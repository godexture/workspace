import { GoMain } from "./generated/go-main";

export interface PluginField {
    name: string;
    type: string;
    help: string;
    default: string;
    choices?: string[];
}

export interface PluginEntry {
    role: string;
    name: string;
    description: string;
    fields: PluginField[];
}

export interface OutputFormat {
    muxer: string;
    extensions: string[];
    codecs: string[];
    defaultCodec: string;
}

export interface Catalog {
    demuxers: PluginEntry[];
    decoders: PluginEntry[];
    filters: PluginEntry[];
    encoders: PluginEntry[];
    muxers: PluginEntry[];
    outputs: OutputFormat[];
}

export interface PluginSpec {
    name: string;
    values?: Record<string, string>;
}

export type FilterSpec = PluginSpec;

export interface ConversionSpec {
    demuxer?: PluginSpec;
    decoder?: PluginSpec;
    filters?: FilterSpec[];
    codec?: string;
    encoder?: PluginSpec;
    muxer: PluginSpec;
    parallelism?: number;
}

export interface NodeStatus {
    id: string;
    role: string;
    plugin: string;
    autoInserted: boolean;
    state: string;
    elapsedMs: number;
    error?: string;
}

export type JobStatus = "running" | "completed" | "failed" | "canceled";

export interface Progress {
    status?: JobStatus;
    error?: string;
    percent: number;
    processedMs: number;
    totalMs: number;
    processedItems: number;
    speedRatio: number;
    elapsedMs: number;
    etaMs: number;
    nodes: NodeStatus[];
}

export interface StreamInfo {
    [key: string]: unknown;
}

export interface PipelineNode {
    ID: string;
    Role: string;
    Plugin: string;
    AutoInserted: boolean;
    Inputs: StreamInfo[];
    Outputs: StreamInfo[];
}

export interface PipelineEdge {
    FromNode: string;
    FromPort: string;
    ToNode: string;
    ToPort: string;
    ProgressSource: boolean;
}

export interface PipelineDescription {
    Nodes: PipelineNode[];
    Edges: PipelineEdge[];
}

// Godexture wraps the worker-mode WASM bindings with a typed, JSON-free API.
// Every call runs in a Web Worker so it never blocks the calling thread;
// long-running conversions are polled via snapshot() rather than callbacks.
export class Godexture {
    private goMain?: GoMain;

    // workerUrl must point at worker.js served/bundled alongside wasm_exec.js
    // and main.wasm (the dist/ output of `bun run build`) -- worker.js loads
    // both via URLs relative to its own location, not __dirname, so this
    // package cannot guess a working default across Node, Bun, and bundlers.
    async init(workerUrl: string): Promise<void> {
        if (this.goMain) return;
        this.goMain = await GoMain.init(workerUrl);
    }

    terminate(): void {
        this.goMain?.terminate();
        this.goMain = undefined;
    }

    private main(): GoMain {
        if (!this.goMain) {
            throw new Error("Godexture not initialized. Call init() first.");
        }
        return this.goMain;
    }

    async getCatalog(): Promise<Catalog> {
        return JSON.parse(await this.main().catalog());
    }

    /** Negotiates a pipeline for input against spec without running it. */
    async resolvePipeline(input: Uint8Array, spec: ConversionSpec): Promise<PipelineDescription> {
        return JSON.parse(await this.main().resolve(input, JSON.stringify(spec)));
    }

    /** Begins a conversion in the background and returns a job ID. */
    async start(input: Uint8Array, spec: ConversionSpec): Promise<string> {
        return this.main().start(input, JSON.stringify(spec));
    }

    /** Polls a job's current progress and outcome. */
    async snapshot(jobId: string): Promise<Progress> {
        return JSON.parse(await this.main().snapshot(jobId));
    }

    async cancel(jobId: string): Promise<void> {
        await this.main().cancel(jobId);
    }

    /** Waits for a job to finish and returns its output. Call at most once per job. */
    async result(jobId: string): Promise<Uint8Array> {
        return this.main().result(jobId);
    }
}
