import axios from "axios";

import type { Catalog, Preset } from "./types";

// Single axios instance used by every Server-mode call (catalog/presets
// lookups here, and conversion requests in conversion/backend/serverBackend).
export const http = axios.create({ baseURL: "/api" });

export async function fetchCatalog(): Promise<Catalog> {
    const { data } = await http.get<Catalog>("/catalog");
    return data;
}

export async function fetchPresets(): Promise<Preset[]> {
    const { data } = await http.get<Preset[]>("/presets");
    return data;
}

export function presetAudioUrl(presetId: string): string {
    return `/api/presets/${encodeURIComponent(presetId)}/audio`;
}

interface ErrorEnvelope {
    error?: { code?: string; message?: string };
}

// apiErrorMessage extracts the server's {error:{message}} envelope from an
// axios error. Requests made with responseType "blob"/"arraybuffer"
// (getResult, preset audio fetches) get their error body back in that same
// binary shape instead of parsed JSON, so those cases are read and parsed
// by hand.
export async function apiErrorMessage(error: unknown): Promise<string> {
    if (!axios.isAxiosError(error)) {
        return error instanceof Error ? error.message : String(error);
    }
    let data: unknown = error.response?.data;
    if (data instanceof Blob) {
        data = parseJSON(await data.text());
    } else if (data instanceof ArrayBuffer) {
        data = parseJSON(new TextDecoder().decode(data));
    }
    return (data as ErrorEnvelope | undefined)?.error?.message ?? error.message;
}

function parseJSON(text: string): unknown {
    try {
        return JSON.parse(text);
    } catch {
        return undefined;
    }
}
