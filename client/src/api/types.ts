export type {
    Catalog,
    PluginEntry,
    PluginField,
    FilterEntry,
    OutputFormat,
    PluginSpec,
    FilterSpec,
    PortRef,
    AuxInputSpec,
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
