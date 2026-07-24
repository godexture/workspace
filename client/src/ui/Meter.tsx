import styles from "./Meter.module.css";

export function Meter({ percent }: { percent: number }) {
    return (
        <div className={styles.track}>
            <div className={styles.fill} style={{ width: `${Math.max(0, Math.min(100, percent))}%` }} />
        </div>
    );
}
