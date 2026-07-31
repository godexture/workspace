import type { ReactNode } from "react";

import styles from "./Panel.module.css";

export interface PanelProps {
    title?: string;
    description?: string;
    actions?: ReactNode;
    bodyClassName?: string;
    className?: string;
    children: ReactNode;
}

export function Panel({ title, description, actions, bodyClassName, className, children }: PanelProps) {
    return (
        <section className={[styles.panel, className].filter(Boolean).join(" ")}>
            {(title || actions) && (
                <header className={styles.header}>
                    <div className={styles.titleGroup}>
                        {title && <h2 className={styles.title}>{title}</h2>}
                        {description && <p className={styles.description}>{description}</p>}
                    </div>
                    {actions && <div className={styles.actions}>{actions}</div>}
                </header>
            )}
            <div className={[styles.body, bodyClassName].filter(Boolean).join(" ")}>{children}</div>
        </section>
    );
}
