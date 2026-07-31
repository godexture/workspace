import { useEffect, useMemo, useRef, useState } from "react";

import type { ConfigurationRequest, ConfigurationResolution } from "../api/types";

export interface ConfigurationTarget {
    role: string;
    name: string;
    parameters?: Record<string, string>;
    resolve: (request: ConfigurationRequest) => Promise<ConfigurationResolution>;
}

interface State {
    key: string;
    valuesKey: string;
    resolution: ConfigurationResolution | null;
    error: string | null;
    loading: boolean;
}

export function useResolution(
    target: ConfigurationTarget | undefined,
    values: Record<string, string>,
    onChange: (values: Record<string, string>) => void,
): State {
    const valuesRef = useRef(values);
    const onChangeRef = useRef(onChange);
    valuesRef.current = values;
    onChangeRef.current = onChange;

    const targetKey = target ? `${target.role}:${target.name}:${stableRecord(target.parameters ?? {})}` : "";
    const [state, setState] = useState<State>({ key: "", valuesKey: "", resolution: null, error: null, loading: false });
    const current = state.key === targetKey ? state.resolution : null;
    const dependencies = useMemo(
        () => [...new Set(current?.fields.flatMap((field) => field.dependsOn ?? []) ?? [])].sort(),
        [current],
    );
    const valuesKey = current
        ? stableRecord(Object.fromEntries(dependencies.map((name) => [name, values[name] ?? ""])))
        : stableRecord(values);
    const request = target?.resolve;
    const parametersKey = stableRecord(target?.parameters ?? {});
    const requestID = useRef(0);

    useEffect(() => {
        if (!target || !request) {
            setState({ key: "", valuesKey: "", resolution: null, error: null, loading: false });
            return;
        }
        if (state.key === targetKey && state.resolution && state.valuesKey === valuesKey) return;
        const id = ++requestID.current;
        setState((previous) => ({
            key: targetKey,
            valuesKey: previous.key === targetKey ? previous.valuesKey : "",
            resolution: previous.key === targetKey ? previous.resolution : null,
            error: null,
            loading: true,
        }));
        const timer = window.setTimeout(() => {
            const requestedValues = { ...valuesRef.current };
            void request({
                role: target.role,
                name: target.name,
                parameters: target.parameters,
                values: requestedValues,
            }).then((resolution) => {
                if (id !== requestID.current) return;
                const resolvedDependencies = [...new Set(resolution.fields.flatMap((field) => field.dependsOn ?? []))].sort();
                const resolvedValuesKey = stableRecord(Object.fromEntries(
                    resolvedDependencies.map((name) => [name, requestedValues[name] ?? ""]),
                ));
                setState({ key: targetKey, valuesKey: resolvedValuesKey, resolution, error: null, loading: false });
                const latest = valuesRef.current;
                const next = { ...latest, ...resolution.updates };
                if (Object.entries(resolution.updates ?? {}).some(([name, value]) => latest[name] !== value)) {
                    onChangeRef.current(next);
                }
            }).catch((error: unknown) => {
                if (id !== requestID.current) return;
                setState((previous) => ({
                    key: targetKey,
                    valuesKey: previous.key === targetKey ? previous.valuesKey : "",
                    resolution: previous.key === targetKey ? previous.resolution : null,
                    error: error instanceof Error ? error.message : String(error),
                    loading: false,
                }));
            });
        }, 80);
        return () => {
            window.clearTimeout(timer);
            requestID.current++;
        };
    }, [request, targetKey, parametersKey, valuesKey]);

    if (!target || state.key !== targetKey) {
        return { key: targetKey, valuesKey: "", resolution: null, error: null, loading: Boolean(target) };
    }
    return state;
}

function stableRecord(values: Record<string, string>): string {
    return JSON.stringify(Object.entries(values).sort(([left], [right]) => left.localeCompare(right)));
}
