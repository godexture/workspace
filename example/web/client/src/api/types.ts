export type {
    Catalog,
    ConfigurationField,
    ConfigurationRequest,
    ConfigurationResolution,
    ConfigurationSlot,
    ConfigurationValueSource,
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
    StreamInfo,
} from "@godexture/js";

export interface Preset {
    id: string;
    name: string;
    filename: string;
    contentType: string;
}
