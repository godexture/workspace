import type { ConfigurationRequest, ConfigurationResolution, ConversionSpec, FilterEntry, PipelineDescription, Preset, Progress } from "../../api/types";

export type InputSource = { kind: "upload"; file: File } | { kind: "preset"; preset: Preset };

export interface ConversionInputs {
    main: InputSource;
    aux: Record<string, InputSource>;
}

export function inputLabel(input: InputSource): string {
    return input.kind === "upload" ? input.file.name : input.preset.name;
}

export type BackendMode = "server" | "client";

// ConversionBackend is implemented once per conversion mode (Server/Client);
// the rest of the app is written against this interface so switching modes
// is just swapping which implementation useConversionJob is given.
export interface ConversionBackend {
    readonly mode: BackendMode;
    describeFilter(name: string, parameters?: Record<string, string>): Promise<FilterEntry>;
    resolveConfiguration(request: ConfigurationRequest): Promise<ConfigurationResolution>;
    resolvePipeline(inputs: ConversionInputs, spec: ConversionSpec): Promise<PipelineDescription>;
    start(inputs: ConversionInputs, spec: ConversionSpec): Promise<string>;
    /** Calls onProgress until the job leaves the "running" state. Returns an unsubscribe function. */
    subscribe(jobId: string, onProgress: (progress: Progress) => void): () => void;
    cancel(jobId: string): Promise<void>;
    getResult(jobId: string): Promise<Blob>;
}
