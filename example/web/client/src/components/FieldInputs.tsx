import type { PluginField } from "../api/types";
import { Field } from "../ui";
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
    const byName = new Map(fields.map((field) => [field.name, field]));
    const visible = fields.filter((field) => isFieldVisible(field, values, byName));
    if (visible.length === 0) return null;
    return (
        <div className={styles.grid}>
            {visible.map((field) => (
                <Field key={field.name} label={field.name} hint={field.help}>
                    <FieldControl
                        field={field}
                        value={values[field.name] ?? ""}
                        onChange={(value) => onChange(field.name, value)}
                    />
                </Field>
            ))}
        </div>
    );
}

function isFieldVisible(
    field: PluginField,
    values: Record<string, string>,
    byName: Map<string, PluginField>,
): boolean {
    if (!field.dependsOn) return true;
    const controller = byName.get(field.dependsOn.field);
    const current = values[field.dependsOn.field] ?? controller?.default ?? "";
    return field.dependsOn.values.includes(current);
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
