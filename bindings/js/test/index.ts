import { Godexture } from "@src/index";

async function main() {
    console.log("Starting test...");
    const godec = new Godexture();
    await godec.init();
    console.log("WASM Initialized!");

    const res = godec.hello();
    console.log("Response from Go:", res);

    if (res !== "Hello Generated!") {
        console.error("Unexpected response:", res);
        process.exit(1);
    } else {
        console.log("Test passed!");
    }
}

main().catch((err) => {
    console.error("Test failed:", err);
    process.exit(1);
});
