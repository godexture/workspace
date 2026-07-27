import styles from "./Tabs.module.css";

export interface TabOption<T extends string> {
    value: T;
    label: string;
}

export interface TabsProps<T extends string> {
    value: T;
    options: TabOption<T>[];
    onChange: (value: T) => void;
}

// Underline-indicator tabs, distinct from the pill-style SegmentedControl:
// used to split a single node's settings into sub-views (e.g. Parameters /
// Advanced) rather than to pick between mutually exclusive top-level modes.
export function Tabs<T extends string>({ value, options, onChange }: TabsProps<T>) {
    return (
        <div className={styles.tabs} role="tablist">
            {options.map((option) => (
                <button
                    key={option.value}
                    type="button"
                    role="tab"
                    aria-selected={option.value === value}
                    className={option.value === value ? styles.active : styles.tab}
                    onClick={() => onChange(option.value)}
                >
                    {option.label}
                </button>
            ))}
        </div>
    );
}
