import { expect, test } from "bun:test";

import type { ConfigurationSlot } from "../src/api/types";
import { resetSliderValues, serializeSliderValues, setSliderValue, sliderValues } from "../src/components/slider";

const slots: ConfigurationSlot[] = [
    { index: 1, label: "100 Hz", default: 0 },
    { index: 0, label: "1 kHz", default: 0 },
    { index: 2, label: "10 kHz", default: 0 },
];

test("slider slots retain their resolved value indexes", () => {
    const values = sliderValues("1,2,3", slots);
    expect(setSliderValue(values, slots[0]!, 4)).toEqual([1, 4, 3]);
});

test("slider values expand, reset, and serialize", () => {
    const values = sliderValues("2", slots);
    expect(values).toEqual([2, 0, 0]);
    expect(serializeSliderValues(resetSliderValues(values, slots))).toBe("0,0,0");
});
