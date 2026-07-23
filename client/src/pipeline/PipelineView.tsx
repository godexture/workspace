import type { NodeStatus, PipelineDescription } from "../api/types";
import styles from "./PipelineView.module.css";

interface PipelineViewProps {
    description: PipelineDescription | null;
    liveNodes?: NodeStatus[];
    error?: string | null;
}

const STATE_LABEL: Record<string, string> = {
    ready: "Pending",
    running: "Running",
    completed: "Completed",
    failed: "Failed",
    unobserved: "",
};

export function PipelineView({
    description,
    liveNodes,
    error,
}: PipelineViewProps) {
    if (error) {
        return (
            <p className={styles.error}>Failed to resolve pipeline: {error}</p>
        );
    }
    if (!description || description.Nodes.length === 0) {
        return (
            <p className={styles.empty}>
                Please select an input and conversion spec to see the resolved
                pipeline.
            </p>
        );
    }

    const live = new Map((liveNodes ?? []).map((node) => [node.id, node]));

    return (
        <div className={styles.flow}>
            {description.Nodes.map((node, index) => {
                const status = live.get(node.ID);
                return (
                    <div className={styles.step} key={node.ID}>
                        <div
                            className={[
                                styles.card,
                                status ? styles[`state_${status.state}`] : "",
                            ].join(" ")}
                        >
                            <div className={styles.role}>
                                {node.Role}
                                {node.AutoInserted && (
                                    <span className={styles.autoBadge}>
                                        Auto-inserted
                                    </span>
                                )}
                            </div>
                            <div className={styles.plugin}>{node.Plugin}</div>
                            {status && status.state !== "unobserved" && (
                                <div className={styles.state}>
                                    {STATE_LABEL[status.state] ?? status.state}
                                    {status.error && (
                                        <span className={styles.nodeError}>
                                            {status.error}
                                        </span>
                                    )}
                                </div>
                            )}
                        </div>
                        {index < description.Nodes.length - 1 && (
                            <div className={styles.arrow}>→</div>
                        )}
                    </div>
                );
            })}
        </div>
    );
}
