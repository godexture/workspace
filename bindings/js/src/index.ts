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

export interface FilterEntry extends PluginEntry {
    parameters: PluginField[];
    inputs: string[];
    outputs: string[];
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
    filters: FilterEntry[];
    encoders: PluginEntry[];
    muxers: PluginEntry[];
    outputs: OutputFormat[];
}

export interface PluginSpec {
    name: string;
    values?: Record<string, string>;
}

export interface PortRef {
    alias: string;
    port?: string;
}

export interface FilterSpec extends PluginSpec {
    alias?: string;
    inputs?: Record<string, PortRef>;
    parameters?: Record<string, string>;
}

export interface AuxInputSpec {
    demuxer?: PluginSpec;
    decoder?: PluginSpec;
}

export interface ConversionSpec {
    demuxer?: PluginSpec;
    decoder?: PluginSpec;
    filters?: FilterSpec[];
    auxInputs?: Record<string, AuxInputSpec>;
    sink?: PortRef;
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
    Stream: StreamInfo;
    ProgressSource: boolean;
}

export interface PipelineDescription {
    Nodes: PipelineNode[];
    Edges: PipelineEdge[];
}

export interface InputSet {
    main: Uint8Array;
    aux?: Record<string, Uint8Array>;
}

// Godexture wraps the worker-mode WASM bindings with a typed, JSON-free API.
// Every call runs in a Web Worker so it never blocks the calling thread.
// start()'s own promise doesn't resolve until the conversion finishes
// (goroutines on js/wasm are scheduled cooperatively, so the worker can't
// hand control back to JS mid-run); its onProgress callback is the only way
// to observe progress before then.
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

    async describeFilter(
        name: string,
        parameters: Record<string, string> = {},
    ): Promise<FilterEntry> {
        return JSON.parse(await this.main().describeFilter(name, parameters));
    }

    /** Negotiates a pipeline for input against spec without running it. */
    async resolvePipeline(inputs: InputSet, spec: ConversionSpec): Promise<PipelineDescription> {
        return JSON.parse(await this.main().resolve(inputs.main, inputs.aux ?? {}, JSON.stringify(spec)));
    }

    /**
     * Begins a conversion in the background under jobId (chosen by the
     * caller) and returns it once the conversion finishes. onProgress is
     * called with live updates in the meantime -- it's the only way to
     * observe progress before then, since the returned promise itself
     * doesn't resolve until the job is done.
     */
    async start(
        jobId: string,
        inputs: InputSet,
        spec: ConversionSpec,
        onProgress: (progress: Progress) => void,
    ): Promise<string> {
        return this.main().start(jobId, inputs.main, inputs.aux ?? {}, JSON.stringify(spec), (data) => {
            onProgress(JSON.parse(data));
        });
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
