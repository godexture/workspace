import styles from "./SegmentedControl.module.css";

export interface SegmentedOption<T extends string> {
    value: T;
    label: string;
}

export interface SegmentedControlProps<T extends string> {
    value: T;
    options: SegmentedOption<T>[];
    onChange: (value: T) => void;
    disabled?: boolean;
}

export function SegmentedControl<T extends string>({ value, options, onChange, disabled }: SegmentedControlProps<T>) {
    return (
        <div className={styles.group}>
            {options.map((option) => (
                <button
                    key={option.value}
                    type="button"
                    disabled={disabled}
                    className={option.value === value ? styles.active : styles.option}
                    onClick={() => onChange(option.value)}
                >
                    {option.label}
                </button>
            ))}
        </div>
    );
}
