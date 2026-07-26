export interface PluginField {
    name: string;
    type: string;
    help: string;
    default: string;
    choices?: string[];
    dependsOn?: { field: string; values: string[] };
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

export interface PluginSpec { name: string; values?: Record<string, string>; }
export interface PortRef { alias: string; port?: string; }
export interface FilterSpec extends PluginSpec { alias?: string; inputs?: Record<string, PortRef>; parameters?: Record<string, string>; }
export interface AuxInputSpec { demuxer?: PluginSpec; decoder?: PluginSpec; }

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
export interface Progress { status?: JobStatus; error?: string; percent: number; processedMs: number; totalMs: number; processedItems: number; speedRatio: number; elapsedMs: number; etaMs: number; nodes: NodeStatus[]; }
export interface StreamInfo { [key: string]: unknown; }
export interface PipelineNode { ID: string; Role: string; Plugin: string; AutoInserted: boolean; Inputs: StreamInfo[]; Outputs: StreamInfo[]; }
export interface PipelineEdge { FromNode: string; FromPort: string; ToNode: string; ToPort: string; Stream: StreamInfo; ProgressSource: boolean; }
export interface PipelineDescription { Nodes: PipelineNode[]; Edges: PipelineEdge[]; }
export interface InputSet { main: Uint8Array; aux?: Record<string, Uint8Array>; }
