import type { PluginField } from "../api/types";
import styles from "./FieldInputs.module.css";

interface FieldInputsProps {
    fields: PluginField[];
    values: Record<string, string>;
    onChange: (name: string, value: string) => void;
}

// FieldInputs renders one control per catalog PluginField, generically
// covering every plugin config (filters, encoders, muxers, ...) without a
// hand-written form per plugin. Values always round-trip as strings, since
// that is the wire format the Go side (cliflag.DecodeStruct) expects.
export function FieldInputs({ fields, values, onChange }: FieldInputsProps) {
    if (fields.length === 0) return null;
    return (
        <div className={styles.grid}>
            {fields.map((field) => (
                <label key={field.name} className={styles.field} title={field.help}>
                    <span className={styles.label}>{field.name}</span>
                    <FieldControl
                        field={field}
                        value={values[field.name] ?? ""}
                        onChange={(value) => onChange(field.name, value)}
                    />
                </label>
            ))}
        </div>
    );
}

function FieldControl({
    field,
    value,
    onChange,
}: {
    field: PluginField;
    value: string;
    onChange: (value: string) => void;
}) {
    if (field.type === "enum" && field.choices) {
        return (
            <select value={value || field.default} onChange={(e) => onChange(e.target.value)}>
                {field.choices.map((choice) => (
                    <option key={choice} value={choice}>
                        {choice}
                    </option>
                ))}
            </select>
        );
    }
    if (field.type === "bool") {
        return (
            <input
                type="checkbox"
                checked={(value || field.default) === "true"}
                onChange={(e) => onChange(e.target.checked ? "true" : "false")}
            />
        );
    }
    if (isNumericType(field.type)) {
        return (
            <input
                type="number"
                placeholder={field.default}
                value={value}
                step={field.type.startsWith("float") ? "any" : "1"}
                onChange={(e) => onChange(e.target.value)}
            />
        );
    }
    return <input type="text" placeholder={field.default} value={value} onChange={(e) => onChange(e.target.value)} />;
}

function isNumericType(type: string): boolean {
    return /^(u?int\d*|float\d*)$/.test(type);
}
