import { useCallback, useState } from "react";

interface HistoryState<T> {
    past: T[];
    present: T;
    future: T[];
}

export interface History<T> {
    value: T;
    set: (next: T) => void;
    reset: (next: T) => void;
    undo: () => void;
    redo: () => void;
    canUndo: boolean;
    canRedo: boolean;
}

export function useHistory<T>(initial: T): History<T> {
    const [state, setState] = useState<HistoryState<T>>({ past: [], present: initial, future: [] });

    const set = useCallback((next: T) => {
        setState((current) => current.present === next
            ? current
            : { past: [...current.past, current.present], present: next, future: [] });
    }, []);

    const reset = useCallback((next: T) => {
        setState({ past: [], present: next, future: [] });
    }, []);

    const undo = useCallback(() => {
        setState((current) => {
            if (current.past.length === 0) return current;
            const present = current.past[current.past.length - 1];
            return { past: current.past.slice(0, -1), present, future: [current.present, ...current.future] };
        });
    }, []);

    const redo = useCallback(() => {
        setState((current) => {
            if (current.future.length === 0) return current;
            const [present, ...future] = current.future;
            return { past: [...current.past, current.present], present, future };
        });
    }, []);

    return {
        value: state.present,
        set,
        reset,
        undo,
        redo,
        canUndo: state.past.length > 0,
        canRedo: state.future.length > 0,
    };
}
