import { useReactFlow, useNodesInitialized } from "@xyflow/react";
import { useEffect, useRef, type RefObject } from "react";

interface FitViewOnReadyProps {
    padding?: number;
    // Fit only the first time nodes measure in, then leave the viewport
    // alone -- for canvases the user pans/zooms/arranges by hand (repeated
    // fitting on every node add would fight their camera). Omit for
    // read-only canvases that should always reframe themselves.
    once?: boolean;
    // The <ReactFlow> root's own wrapper. A canvas mounted behind
    // `display:none` (e.g. the Result tab before it's selected) measures
    // its nodes at 0x0 while hidden, which already satisfies
    // useNodesInitialized -- so when the tab is later revealed, that flag
    // never flips again and no refit happens even though real dimensions
    // just became available. Passing the container lets this watch for
    // that reveal directly instead of relying on the node-measurement
    // signal for it.
    containerRef?: RefObject<HTMLElement | null>;
}

// The `fitView` prop only fits once, synchronously at mount -- before
// custom nodes (sized by CSS, measured via ResizeObserver) have reported
// real dimensions, so that initial fit runs against zero-size nodes and
// silently leaves the graph at its raw layout coordinates instead of
// centered. Render this as a child of <ReactFlow> to fit once nodes are
// actually measured, and again whenever the node set changes.
export function FitViewOnReady({ padding = 0.15, once = false, containerRef }: FitViewOnReadyProps) {
    const { fitView } = useReactFlow();
    const nodesInitialized = useNodesInitialized();
    const fired = useRef(false);

    useEffect(() => {
        if (!nodesInitialized) return;
        if (once && fired.current) return;
        fired.current = true;
        void fitView({ padding, duration: 150 });
    }, [nodesInitialized, fitView, padding, once]);

    useEffect(() => {
        if (once) return;
        const el = containerRef?.current;
        if (!el) return;
        const observer = new ResizeObserver((entries) => {
            const { width, height } = entries[0].contentRect;
            if (width > 0 && height > 0) void fitView({ padding, duration: 0 });
        });
        observer.observe(el);
        return () => observer.disconnect();
    }, [containerRef, fitView, padding, once]);

    return null;
}
