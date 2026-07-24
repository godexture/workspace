import axios from "axios";

import { apiErrorMessage, http } from "../../api/client";
import type { ConversionSpec, FilterEntry, PipelineDescription, Progress } from "../../api/types";
import type { ConversionBackend, ConversionInputs, InputSource } from "./types";

function inputReference(input: InputSource): { kind: "file" } | { kind: "preset"; presetId: string } {
    return input.kind === "upload"
        ? { kind: "file" }
        : { kind: "preset", presetId: input.preset.id };
}

function buildFormData(inputs: ConversionInputs, spec: ConversionSpec): FormData {
    const form = new FormData();
    form.set("spec", JSON.stringify(spec));
    form.set("inputs", JSON.stringify({
        main: inputReference(inputs.main),
        aux: Object.fromEntries(
            Object.entries(inputs.aux).map(([name, input]) => [name, inputReference(input)]),
        ),
    }));
    if (inputs.main.kind === "upload") {
        form.set("main", inputs.main.file);
    }
    for (const [name, input] of Object.entries(inputs.aux)) {
        if (input.kind === "upload") {
            form.set(`aux:${name}`, input.file);
        }
    }
    return form;
}

export const serverBackend: ConversionBackend = {
    mode: "server",

    async describeFilter(name, parameters = {}) {
        try {
            const { data } = await http.post<FilterEntry>("/filters/describe", { name, parameters });
            return data;
        } catch (err) {
            throw new Error(await apiErrorMessage(err));
        }
    },

    async resolvePipeline(inputs, spec) {
        try {
            const { data } = await http.post<PipelineDescription>("/pipelines/resolve", buildFormData(inputs, spec));
            return data;
        } catch (err) {
            throw new Error(await apiErrorMessage(err));
        }
    },

    async start(inputs, spec) {
        try {
            const { data } = await http.post<{ id: string }>("/conversions", buildFormData(inputs, spec));
            return data.id;
        } catch (err) {
            throw new Error(await apiErrorMessage(err));
        }
    },

    // axios has no server-sent-events support (it is a request/response
    // client); EventSource is the standard browser API for a long-lived
    // event stream, so SSE subscriptions still go through it directly.
    subscribe(jobId, onProgress) {
        const source = new EventSource(`/api/conversions/${jobId}/events`);
        source.onmessage = (event) => {
            const progress = JSON.parse(event.data) as Progress;
            onProgress(progress);
            if (progress.status && progress.status !== "running") {
                source.close();
            }
        };
        // The server always sends a final event and closes the stream
        // itself. An error here means the connection dropped before that;
        // the job keeps running server-side and a later subscribe() call
        // can resume watching it, so just stop this stream.
        source.onerror = () => source.close();
        return () => source.close();
    },

    async cancel(jobId) {
        try {
            await http.delete(`/conversions/${jobId}`);
        } catch (err) {
            if (axios.isAxiosError(err) && err.response?.status === 404) return;
            throw new Error(await apiErrorMessage(err));
        }
    },

    async getResult(jobId) {
        try {
            const { data } = await http.get<Blob>(`/conversions/${jobId}/result`, { responseType: "blob" });
            return data;
        } catch (err) {
            throw new Error(await apiErrorMessage(err));
        }
    },
};
