import { useEffect } from "react";

export interface ShortcutBinding {
    key: string;
    mod?: boolean;
    shift?: boolean;
    alt?: boolean;
    handler: () => void;
}

export function useKeyboardShortcuts(bindings: ShortcutBinding[], enabled = true) {
    useEffect(() => {
        if (!enabled) return;

        function onKeyDown(event: KeyboardEvent) {
            if (isEditableTarget(event.target)) return;
            const mod = event.ctrlKey || event.metaKey;
            const binding = bindings.find((candidate) =>
                candidate.key.toLowerCase() === event.key.toLowerCase() &&
                Boolean(candidate.mod) === mod &&
                Boolean(candidate.shift) === event.shiftKey &&
                Boolean(candidate.alt) === event.altKey,
            );
            if (!binding) return;
            event.preventDefault();
            binding.handler();
        }

        window.addEventListener("keydown", onKeyDown);
        return () => window.removeEventListener("keydown", onKeyDown);
    }, [bindings, enabled]);
}

// Lets native undo/redo inside text fields take priority over app-level shortcuts.
function isEditableTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false;
    return target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable;
}
