import axios from "axios";

import { Godexture } from "@godexture/js";

import { apiErrorMessage, presetAudioUrl } from "../../api/client";
import type { ConversionBackend, InputSource } from "./types";

// worker.js/wasm_exec.js/main.wasm are copied here by `bun run prepare-wasm`
// (see scripts/copy-wasm-assets.ts) so they're served unhashed, side by
// side, at a stable path -- worker.js loads the other two by plain
// filename relative to its own URL.
const WORKER_URL = "/wasm/worker.js";
const POLL_INTERVAL_MS = 250;

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

export const clientBackend: ConversionBackend = {
    mode: "client",

    async resolvePipeline(input, spec) {
        const [godec, bytes] = await Promise.all([getClient(), readInput(input)]);
        return godec.resolvePipeline({ main: bytes }, spec);
    },

    async start(input, spec) {
        const [godec, bytes] = await Promise.all([getClient(), readInput(input)]);
        return godec.start({ main: bytes }, spec);
    },

    subscribe(jobId, onProgress) {
        let cancelled = false;
        void (async () => {
            const godec = await getClient();
            while (!cancelled) {
                const progress = await godec.snapshot(jobId);
                if (cancelled) return;
                onProgress(progress);
                if (progress.status && progress.status !== "running") return;
                await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
            }
        })();
        return () => {
            cancelled = true;
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
