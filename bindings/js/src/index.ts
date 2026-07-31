import { GoMain } from "./generated/go-main";
import type { Catalog, ConfigurationRequest, ConfigurationResolution, ConversionSpec, FilterEntry, InputSet, PipelineDescription, Progress } from "./types";

export type {
    AuxInputSpec,
    Catalog,
    ConfigurationField,
    ConfigurationRequest,
    ConfigurationResolution,
    ConfigurationSlot,
    ConfigurationValueSource,
    ConversionSpec,
    FilterEntry,
    FilterSpec,
    InputSet,
    JobStatus,
    NodeStatus,
    OutputFormat,
    PipelineDescription,
    PipelineEdge,
    PipelineNode,
    PluginEntry,
    PluginField,
    PluginSpec,
    PortRef,
    Progress,
    StreamInfo,
} from "./types";

export class Godexture {
    private goMain?: GoMain;

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

    async describeFilter(name: string, parameters: Record<string, string> = {}): Promise<FilterEntry> {
        return JSON.parse(await this.main().describeFilter(name, parameters));
    }

    async resolveConfiguration(request: ConfigurationRequest): Promise<ConfigurationResolution> {
        return JSON.parse(await this.main().resolveConfiguration(
            request.role,
            request.name,
            request.parameters ?? {},
            request.values ?? {},
        ));
    }

    async resolvePipeline(inputs: InputSet, spec: ConversionSpec): Promise<PipelineDescription> {
        return JSON.parse(await this.main().resolve(inputs.main, inputs.aux ?? {}, JSON.stringify(spec)));
    }

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

    async snapshot(jobId: string): Promise<Progress> {
        return JSON.parse(await this.main().snapshot(jobId));
    }

    async cancel(jobId: string): Promise<void> {
        await this.main().cancel(jobId);
    }

    async result(jobId: string): Promise<Uint8Array> {
        return this.main().result(jobId);
    }
}
