function writeString(view: DataView, offset: number, value: string) {
    for (let index = 0; index < value.length; index++) {
        view.setUint8(offset + index, value.charCodeAt(index));
    }
}

export function wavFile(audio: AudioBuffer, name: string): File {
    const channels = audio.numberOfChannels;
    const frames = audio.length;
    const bytesPerSample = 2;
    const blockAlign = channels * bytesPerSample;
    const dataSize = frames * blockAlign;
    const buffer = new ArrayBuffer(44 + dataSize);
    const view = new DataView(buffer);

    writeString(view, 0, "RIFF");
    view.setUint32(4, 36 + dataSize, true);
    writeString(view, 8, "WAVEfmt ");
    view.setUint32(16, 16, true);
    view.setUint16(20, 1, true);
    view.setUint16(22, channels, true);
    view.setUint32(24, audio.sampleRate, true);
    view.setUint32(28, audio.sampleRate * blockAlign, true);
    view.setUint16(32, blockAlign, true);
    view.setUint16(34, 16, true);
    writeString(view, 36, "data");
    view.setUint32(40, dataSize, true);

    const samples = Array.from({ length: channels }, (_, channel) => audio.getChannelData(channel));
    let offset = 44;
    for (let frame = 0; frame < frames; frame++) {
        for (const channel of samples) {
            const sample = Math.max(-1, Math.min(1, channel[frame]!));
            view.setInt16(offset, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
            offset += bytesPerSample;
        }
    }

    return new File([buffer], name, { type: "audio/wav", lastModified: Date.now() });
}

export async function recordingFile(chunks: Blob[], name: string): Promise<File> {
    const recording = new Blob(chunks);
    const context = new AudioContext();
    try {
        const audio = await context.decodeAudioData(await recording.arrayBuffer());
        return wavFile(audio, name);
    } finally {
        await context.close();
    }
}
