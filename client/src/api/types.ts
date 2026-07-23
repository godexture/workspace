export type {
    Catalog,
    PluginEntry,
    PluginField,
    OutputFormat,
    PluginSpec,
    FilterSpec,
    ConversionSpec,
    Progress,
    JobStatus,
    NodeStatus,
    PipelineDescription,
    PipelineNode,
    PipelineEdge,
} from "@godexture/js";

export interface Preset {
    id: string;
    name: string;
    filename: string;
    contentType: string;
}
