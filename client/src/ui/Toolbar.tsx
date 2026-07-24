import type { ReactNode } from "react";

import styles from "./Toolbar.module.css";

export function Toolbar({ children, className }: { children: ReactNode; className?: string }) {
    return <div className={[styles.toolbar, className].filter(Boolean).join(" ")}>{children}</div>;
}

export function ToolbarGroup({ children }: { children: ReactNode }) {
    return <div className={styles.group}>{children}</div>;
}
