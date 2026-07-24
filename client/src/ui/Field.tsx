import type { ReactNode } from "react";

import styles from "./Field.module.css";

export interface FieldProps {
    label: string;
    hint?: string;
    children: ReactNode;
    className?: string;
}

export function Field({ label, hint, children, className }: FieldProps) {
    return (
        <label className={[styles.field, className].filter(Boolean).join(" ")} title={hint}>
            <span className={styles.label}>{label}</span>
            {children}
        </label>
    );
}
