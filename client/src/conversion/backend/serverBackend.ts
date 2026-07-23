import axios from "axios";

import { apiErrorMessage, http } from "../../api/client";
import type { ConversionSpec, PipelineDescription, Progress } from "../../api/types";
import type { ConversionBackend, InputSource } from "./types";

function buildFormData(input: InputSource, spec: ConversionSpec): FormData {
    const form = new FormData();
    form.set("spec", JSON.stringify(spec));
    if (input.kind === "upload") {
        form.set("file", input.file);
    } else {
        form.set("presetId", input.preset.id);
    }
    return form;
}

export const serverBackend: ConversionBackend = {
    mode: "server",

    async resolvePipeline(input, spec) {
        try {
            const { data } = await http.post<PipelineDescription>("/pipelines/resolve", buildFormData(input, spec));
            return data;
        } catch (err) {
            throw new Error(await apiErrorMessage(err));
        }
    },

    async start(input, spec) {
        try {
            const { data } = await http.post<{ id: string }>("/conversions", buildFormData(input, spec));
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
