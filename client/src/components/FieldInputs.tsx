import type { PluginField } from "../api/types";
import { Field } from "../ui";
import { type ConfigurationTarget, useResolution } from "./resolution";
import { Sliders } from "./Sliders";
import styles from "./FieldInputs.module.css";

interface FieldInputsProps {
    configuration?: ConfigurationTarget;
    fields: PluginField[];
    values: Record<string, string>;
    onChange: (values: Record<string, string>) => void;
}

// FieldInputs renders one control per catalog PluginField, generically
// covering every plugin config (filters, encoders, muxers, ...) without a
// hand-written form per plugin. Values always round-trip as strings, since
// that is the wire format the Go side (cliflag.DecodeStruct) expects.
export function FieldInputs({ configuration, fields, values, onChange }: FieldInputsProps) {
    const byName = new Map(fields.map((field) => [field.name, field]));
    const visible = fields.filter((field) => isFieldVisible(field, values, byName));
    const resolved = useResolution(configuration, values, onChange);
    const dynamic = new Map(resolved.resolution?.fields.map((field) => [field.name, field]) ?? []);
    const update = (name: string, value: string) => {
        onChange({ ...values, [name]: value });
    };
    const valueFor = (field: PluginField) => {
        const explicit = values[field.name];
        if (explicit !== undefined) return explicit;
        const resolution = resolved.resolution;
        return resolution && resolution.sources[field.name] !== "default"
            ? resolution.values[field.name] ?? ""
            : "";
    };
    if (visible.length === 0) return null;
    return (
        <div className={styles.grid}>
            {visible.map((field) => field.editor === "sliders" ? (
                <Sliders
                    key={field.name}
                    field={field}
                    state={dynamic.get(field.name)}
                    value={values[field.name] ?? resolved.resolution?.values[field.name] ?? field.default}
                    error={resolved.error}
                    loading={resolved.loading}
                    onChange={(value) => update(field.name, value)}
                />
            ) : (
                <Field key={field.name} label={field.name} hint={field.help}>
                    <FieldControl
                        field={field}
                        value={valueFor(field)}
                        onChange={(value) => update(field.name, value)}
                    />
                </Field>
            ))}
        </div>
    );
}

export function isFieldVisible(
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
                className={styles.checkbox}
                checked={(value || field.default) === "true"}
                onChange={(e) => onChange(e.target.checked ? "true" : "false")}
            />
        );
    }
    if (isNumericType(field.type)) {
        return (
            <input
                type="number"
                className={styles.numeric}
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
