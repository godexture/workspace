import { Godexture } from "../src/index";

async function main() {
    console.log("Starting test...");
    const godec = new Godexture();
    await godec.init();
    console.log("WASM Initialized!");

    const fs = require("fs");
    const path = require("path");

    const inputPath = path.join(__dirname, "../../../plugins/codec-mp3/test/testdata/l3-hecommon.mp3");
    console.log(`Reading input from ${inputPath}`);
    const inputBuffer = fs.readFileSync(inputPath);
    
    console.log("Converting to WAV...");
    const outputBuffer = godec.convert(new Uint8Array(inputBuffer), "wav");
    
    console.log(`Converted to WAV! Size: ${outputBuffer.length} bytes`);
    
    const outputPath = path.join(__dirname, "../test_output.wav");
    fs.writeFileSync(outputPath, outputBuffer);
    console.log(`Wrote output to ${outputPath}`);
    
    if (outputBuffer.length > 0) {
        console.log("Test passed!");
    } else {
        console.error("Test failed: Output buffer is empty");
        process.exit(1);
    }
}

main().catch((err) => {
    console.error("Test failed:", err);
    process.exit(1);
});
