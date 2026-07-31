import type { ConfigurationField, PluginField } from "../api/types";
import { resetSliderValues, serializeSliderValues, setSliderValue, sliderValues } from "./slider";
import styles from "./Sliders.module.css";

interface SlidersProps {
    field: PluginField;
    state?: ConfigurationField;
    value: string;
    error: string | null;
    loading: boolean;
    onChange: (value: string) => void;
}

export function Sliders({ field, state, value, error, loading, onChange }: SlidersProps) {
    const slots = state?.slots ?? [];
    const range = state?.range;
    if (error && slots.length === 0) {
        return <Message field={field} value={error} invalid />;
    }
    if (!range || slots.length === 0) {
        return <Message field={field} value={loading ? "Resolving controls…" : "No controls available."} />;
    }
    const values = sliderValues(value, slots);
    const unit = state?.unit ? ` ${state.unit}` : "";
    const format = (current: number) => `${current > 0 ? "+" : ""}${current}${unit}`;
    const setValue = (slotIndex: number, nextValue: number) => {
        const next = setSliderValue(values, slots[slotIndex]!, nextValue);
        onChange(serializeSliderValues(next));
    };
    return (
        <fieldset className={styles.sliders}>
            <legend title={field.help}>{field.name}</legend>
            <button
                type="button"
                className={styles.reset}
                onClick={() => onChange(serializeSliderValues(resetSliderValues(values, slots)))}
            >
                Reset
            </button>
            <div className={styles.scale} aria-hidden="true">
                <span>{format(range.max)}</span>
                <span>{format(0)}</span>
                <span>{format(range.min)}</span>
            </div>
            <div className={styles.items}>
                {slots.map((slot, index) => {
                    const current = values[slot.index] ?? slot.default;
                    const sliderValue = Math.max(range.min, Math.min(range.max, current));
                    return (
                        <label className={styles.item} key={slot.index}>
                            <output>{format(current)}</output>
                            <input
                                type="range"
                                min={range.min}
                                max={range.max}
                                step={range.step}
                                value={sliderValue}
                                aria-label={`${slot.label} ${field.name}`}
                                onChange={(event) => setValue(index, Number(event.target.value))}
                                onDoubleClick={() => setValue(index, slot.default)}
                            />
                            <span>{slot.label}</span>
                        </label>
                    );
                })}
            </div>
            {error && <p className={styles.invalid}>{error}</p>}
        </fieldset>
    );
}

function Message({ field, value, invalid = false }: { field: PluginField; value: string; invalid?: boolean }) {
    return (
        <fieldset className={styles.sliders}>
            <legend title={field.help}>{field.name}</legend>
            <p className={invalid ? styles.invalid : styles.message}>{value}</p>
        </fieldset>
    );
}
