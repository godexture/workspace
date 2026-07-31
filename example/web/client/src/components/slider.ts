import type { ConfigurationSlot } from "../api/types";

export function sliderValues(raw: string, slots: ConfigurationSlot[]): number[] {
    const parsed = raw.split(",").map((part) => Number(part.trim()));
    const length = slots.reduce((maximum, slot) => Math.max(maximum, slot.index + 1), parsed.length);
    return Array.from({ length }, (_, index) => Number.isFinite(parsed[index]) ? parsed[index]! : 0);
}

export function setSliderValue(values: number[], slot: ConfigurationSlot, value: number): number[] {
    const next = [...values];
    next[slot.index] = value;
    return next;
}

export function resetSliderValues(values: number[], slots: ConfigurationSlot[]): number[] {
    const next = [...values];
    for (const slot of slots) {
        next[slot.index] = slot.default;
    }
    return next;
}

export function serializeSliderValues(values: number[]): string {
    return values.map((value) => String(Object.is(value, -0) ? 0 : value)).join(",");
}
