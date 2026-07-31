import { expect, test } from "bun:test";

import { wavFile } from "../src/audio/recording";

test("microphone audio is encoded as a PCM WAV file", async () => {
    const channels = [new Float32Array([0, 1, -1]), new Float32Array([0.5, -0.5, 0])];
    const audio = {
        numberOfChannels: channels.length,
        length: channels[0]!.length,
        sampleRate: 48_000,
        getChannelData: (channel: number) => channels[channel]!,
    } as AudioBuffer;

    const file = wavFile(audio, "recording.wav");
    const view = new DataView(await file.arrayBuffer());

    expect(file.type).toBe("audio/wav");
    expect(view.getUint32(24, true)).toBe(48_000);
    expect(view.getUint16(22, true)).toBe(2);
    expect(view.getUint32(40, true)).toBe(12);
    expect(view.getInt16(44, true)).toBe(0);
    expect(view.getInt16(46, true)).toBe(16_383);
    expect(view.getInt16(48, true)).toBe(32_767);
    expect(view.getInt16(50, true)).toBe(-16_384);
});
