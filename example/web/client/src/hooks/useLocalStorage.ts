import { useEffect, useState } from "react";

export function useLocalStorage<T>(key: string, initialValue: T | (() => T)): [T, (val: T | ((prev: T) => T)) => void] {
    const [value, setValue] = useState<T>(() => {
        try {
            const item = window.localStorage.getItem(key);
            if (item) {
                return JSON.parse(item);
            }
        } catch (e) {
            console.error("Error reading localStorage", e);
        }
        return typeof initialValue === "function" ? (initialValue as () => T)() : initialValue;
    });

    useEffect(() => {
        try {
            window.localStorage.setItem(key, JSON.stringify(value));
        } catch (e) {
            console.error("Error setting localStorage", e);
        }
    }, [key, value]);

    return [value, setValue];
}
