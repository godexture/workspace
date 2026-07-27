import axios from "axios";

import { Godexture } from "@godexture/js";

import { apiErrorMessage, presetAudioUrl } from "../../api/client";
import type { Progress } from "../../api/types";
import type { ConversionBackend, ConversionInputs, InputSource } from "./types";

// worker.js/wasm_exec.js/main.wasm are copied here by `bun run prepare-wasm`
// (see scripts/copy-wasm-assets.ts) so they're served unhashed, side by
// side, at a stable path -- worker.js loads the other two by plain
// filename relative to its own URL.
const WORKER_URL = "/wasm/worker.js";

let client: Promise<Godexture> | null = null;

function getClient(): Promise<Godexture> {
    if (!client) {
        const godec = new Godexture();
        client = godec.init(WORKER_URL).then(() => godec);
    }
    return client;
}

async function readInput(input: InputSource): Promise<Uint8Array> {
    if (input.kind === "upload") {
        return new Uint8Array(await input.file.arrayBuffer());
    }
    try {
        const { data } = await axios.get<ArrayBuffer>(presetAudioUrl(input.preset.id), { responseType: "arraybuffer" });
        return new Uint8Array(data);
    } catch (err) {
        throw new Error(`プリセットの取得に失敗しました (${input.preset.name}): ${await apiErrorMessage(err)}`);
    }
}

async function readInputs(inputs: ConversionInputs): Promise<{ main: Uint8Array; aux: Record<string, Uint8Array> }> {
    const entries = await Promise.all(
        Object.entries(inputs.aux).map(async ([name, input]) => [name, await readInput(input)] as const),
    );
    return { main: await readInput(inputs.main), aux: Object.fromEntries(entries) };
}

function failedProgress(message: string): Progress {
    return {
        status: "failed",
        error: message,
        percent: -1,
        processedMs: 0,
        totalMs: 0,
        processedItems: 0,
        speedRatio: 0,
        elapsedMs: 0,
        etaMs: 0,
        nodes: [],
    };
}

// Godexture.start() doesn't resolve until the conversion finishes (running
// it all happens synchronously inside the WASM call, cooperatively
// scheduled with no real preemption), so progress can only reach us through
// the onProgress callback passed into that same call -- there's no later
// point at which a listener could still attach. jobListeners lets
// subscribe() (called right after start() returns a jobId) register in
// time to catch those pushes; lastProgress replays the most recent one in
// case a push arrives before subscribe() runs.
const jobListeners = new Map<string, (progress: Progress) => void>();
const lastProgress = new Map<string, Progress>();

function emitProgress(jobId: string, progress: Progress): void {
    lastProgress.set(jobId, progress);
    jobListeners.get(jobId)?.(progress);
}

export const clientBackend: ConversionBackend = {
    mode: "client",

    async describeFilter(name, parameters = {}) {
        const godec = await getClient();
        return godec.describeFilter(name, parameters);
    },

    async resolveConfiguration(request) {
        const godec = await getClient();
        return godec.resolveConfiguration(request);
    },

    async resolvePipeline(inputs, spec) {
        const [godec, bytes] = await Promise.all([getClient(), readInputs(inputs)]);
        return godec.resolvePipeline(bytes, spec);
    },

    async start(inputs, spec) {
        const [godec, bytes] = await Promise.all([getClient(), readInputs(inputs)]);
        const jobId = crypto.randomUUID();
        // Fire-and-forget: awaiting this would block until the conversion is
        // done, which would delay subscribe() (called right after this
        // returns) past every progress push it's meant to catch.
        void godec
            .start(jobId, bytes, spec, (progress) => emitProgress(jobId, progress))
            .catch((err) => emitProgress(jobId, failedProgress(err instanceof Error ? err.message : String(err))));
        return jobId;
    },

    subscribe(jobId, onProgress) {
        jobListeners.set(jobId, onProgress);
        const cached = lastProgress.get(jobId);
        if (cached) onProgress(cached);
        return () => {
            jobListeners.delete(jobId);
            lastProgress.delete(jobId);
        };
    },

    async cancel(jobId) {
        const godec = await getClient();
        await godec.cancel(jobId);
    },

    async getResult(jobId) {
        const godec = await getClient();
        const bytes = await godec.result(jobId);
        return new Blob([bytes.slice()]);
    },
};
