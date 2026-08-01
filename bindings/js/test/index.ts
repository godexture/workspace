import * as fs from "node:fs";
import * as path from "node:path";

import { Godexture } from "../src/index";

// Bun's Worker implementation does not yet support importScripts(), which
// the generated (classic) worker.js relies on to load wasm_exec.js. This is
// a real browser feature and works there (verified manually); it is just
// not runnable under Bun yet. Tracked upstream: oven-sh/bun#11989.
// Skip gracefully instead of failing until that lands.
const KNOWN_BUN_WORKER_LIMITATION = "importScripts";

async function main() {
    console.log("Starting test...");
    const godec = new Godexture();
    await godec.init(path.join(__dirname, "../src/generated/worker.js"));
    console.log("Worker initialized!");

    const catalog = await godec.getCatalog();
    console.log(`Catalog: ${catalog.demuxers.length} demuxers, ${catalog.filters.length} filters`);
    if (catalog.demuxers.length === 0 || catalog.muxers.length === 0) {
        throw new Error("catalog is missing demuxers or muxers");
    }

    const inputPath = path.join(__dirname, "../../../plugin/mp3/test/testdata/l3-hecommon.mp3");
    console.log(`Reading input from ${inputPath}`);
    const input = new Uint8Array(fs.readFileSync(inputPath));

    const spec = { muxer: { name: "wav" } };

    const resolved = await godec.resolvePipeline({ main: input }, spec);
    console.log(`Resolved pipeline: ${resolved.Nodes.map((n) => n.Plugin).join(" -> ")}`);

    console.log("Converting to WAV...");
    const jobId = crypto.randomUUID();
    let progressCalls = 0;
    let lastProgress: Awaited<ReturnType<typeof godec.snapshot>> | undefined;
    await godec.start(jobId, { main: input }, spec, (progress) => {
        progressCalls++;
        lastProgress = progress;
    });
    console.log(`Received ${progressCalls} progress update(s)`);
    if (lastProgress?.status !== "completed") {
        throw new Error(`job did not complete: status=${lastProgress?.status}`);
    }
    const output = await godec.result(jobId);
    godec.terminate();

    console.log(`Converted to WAV! Size: ${output.length} bytes`);
    const outputPath = path.join(__dirname, "../test_output.wav");
    fs.writeFileSync(outputPath, output);
    console.log(`Wrote output to ${outputPath}`);

    if (output.length === 0) {
        throw new Error("output buffer is empty");
    }
    console.log("Test passed!");
}

main().catch((err) => {
    const message = err && err.message ? String(err.message) : String(err);
    if (message.includes(KNOWN_BUN_WORKER_LIMITATION)) {
        console.warn(`Skipping: Bun Worker does not support importScripts() yet (${message}).`);
        console.warn("This path is verified manually in a real browser (Chromium).");
        process.exit(0);
    }
    console.error("Test failed:", err);
    process.exit(1);
});
