import type { ButtonHTMLAttributes } from "react";

import styles from "./Button.module.css";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
    variant?: "default" | "primary" | "ghost" | "danger";
}

export function Button({ variant = "default", className, ...props }: ButtonProps) {
    const variantClass = variant !== "default" ? styles[variant] : "";
    return <button type="button" className={[styles.button, variantClass, className].filter(Boolean).join(" ")} {...props} />;
}
