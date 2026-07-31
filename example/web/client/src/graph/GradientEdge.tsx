import { BaseEdge, getBezierPath, type Edge, type EdgeProps } from "@xyflow/react";

export interface GradientEdgeData extends Record<string, unknown> {
    sourceColor: string;
    targetColor: string;
}

export type GradientFlowEdge = Edge<GradientEdgeData>;

// Colors the edge from its source node's kind color to its target node's,
// so a wire reads as belonging to both ends rather than an arbitrary single
// color. Registered as a custom edge type since the built-in bezier edge
// only takes a flat `style.stroke`.
export function GradientEdge({
    id,
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    markerEnd,
    style,
    data,
}: EdgeProps<GradientFlowEdge>) {
    const [path] = getBezierPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition });
    const gradientId = `edge-gradient-${id}`;
    const sourceColor = data?.sourceColor ?? "var(--color-muted)";
    const targetColor = data?.targetColor ?? "var(--color-muted)";
    return (
        <>
            <defs>
                <linearGradient id={gradientId} gradientUnits="userSpaceOnUse" x1={sourceX} y1={sourceY} x2={targetX} y2={targetY}>
                    <stop offset="0%" stopColor={sourceColor} />
                    <stop offset="100%" stopColor={targetColor} />
                </linearGradient>
            </defs>
            <BaseEdge id={id} path={path} markerEnd={markerEnd} style={{ ...style, stroke: `url(#${gradientId})` }} />
        </>
    );
}
