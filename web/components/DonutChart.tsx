import React, { useMemo, useRef, useState } from "react";

export type DonutDatum = {
  label: string;
  value: number;
  color?: string;
};

interface DonutChartProps {
  data: DonutDatum[];
  size?: number; // svg width/height
  strokeWidth?: number;
  centerLabel?: string;
  centerSubLabel?: string;
  className?: string;
  valueFormatter?: (value: number) => string;
}

export default function DonutChart({
  data,
  size = 220,
  strokeWidth = 22,
  centerLabel,
  centerSubLabel,
  className = "",
  valueFormatter,
}: DonutChartProps) {
  const hoverGrow = 6;
  const margin = 6;
  const maxStroke = strokeWidth + hoverGrow;
  const radius = (size - maxStroke - margin * 2) / 2;
  const circumference = 2 * Math.PI * radius;
  const calloutPadding = Math.max(32, size * 0.25);
  const canvasSize = size + calloutPadding * 2;
  const containerRef = useRef<HTMLDivElement>(null);
  const [hovered, setHovered] = useState<number | null>(null);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null);

  const palette = useMemo(() => [
    "#6366f1", // indigo-500
    "#22c55e", // green-500
    "#ef4444", // red-500
    "#06b6d4", // cyan-500
    "#f59e0b", // amber-500
    "#a855f7", // purple-500
    "#14b8a6", // teal-500
    "#3b82f6", // blue-500
    "#f97316", // orange-500
  ], []);

  const processed = useMemo(() => {
    const total = data.reduce((s, d) => s + Math.max(0, d.value), 0);
    let offset = 0;
    let startAngle = 0; // radians
    const segments = data.map((d, i) => {
      const value = Math.max(0, d.value);
      const frac = total > 0 ? value / total : 0;
      const length = frac * circumference;
      const angle = frac * Math.PI * 2;
      const endAngle = startAngle + angle;
      const seg = {
        label: d.label,
        value,
        frac,
        length,
        dasharray: `${length} ${circumference - length}`,
        dashoffset: circumference - offset,
        color: d.color || palette[i % palette.length],
        startAngle,
        endAngle,
        midAngle: startAngle + angle / 2,
      };
      offset += length;
      startAngle = endAngle;
      return seg;
    });
    return { total, segments };
  }, [data, circumference, palette]);

  const outerR = radius + strokeWidth / 2; // outer bound for wedge
  const START_ANGLE = (3 * Math.PI) / 4;
  const toXY = (r: number, angle: number) => {
    const shifted = angle - START_ANGLE;
    return { x: Math.cos(shifted) * r, y: Math.sin(shifted) * r };
  };
  const arcPath = (a0: number, a1: number, r: number) => {
    const p0 = toXY(r, a0);
    const p1 = toXY(r, a1);
    const large = a1 - a0 > Math.PI ? 1 : 0;
    return `M 0 0 L ${p0.x.toFixed(3)} ${p0.y.toFixed(3)} A ${r} ${r} 0 ${large} 1 ${p1.x.toFixed(3)} ${p1.y.toFixed(3)} Z`;
  };

  const labelColumnOffset = radius + strokeWidth / 2 + size * 0.4;
  const connectorLead = Math.max(14, size * 0.06);
  const labelOffset = 10;
  const minLabelGap = 18;

  type CalloutLayout = {
    idx: number;
    direction: 1 | -1;
    connectorStart: { x: number; y: number };
    radialPoint: { x: number; y: number };
    labelX: number;
    baseY: number;
    adjustedY: number;
  };

  const getTooltipText = (segmentIndex: number) => {
    const seg = processed.segments[segmentIndex];
    const formattedValue = valueFormatter ? valueFormatter(seg.value) : String(seg.value);
    const percent = Math.round((seg.frac || 0) * 100);
    return `${seg.label}: ${formattedValue} (${percent}%)`;
  };

  const showTooltip = (segmentIndex: number, event: React.MouseEvent<Element, MouseEvent>) => {
    setHovered(segmentIndex);
    const rect = containerRef.current?.getBoundingClientRect();
    const x = event.clientX - (rect?.left || 0);
    const y = event.clientY - (rect?.top || 0);
    setTooltip({ x, y, text: getTooltipText(segmentIndex) });
  };

  const moveTooltip = (event: React.MouseEvent<Element, MouseEvent>) => {
    if (!tooltip) return;
    const rect = containerRef.current?.getBoundingClientRect();
    const x = event.clientX - (rect?.left || 0);
    const y = event.clientY - (rect?.top || 0);
    setTooltip((prev) => (prev ? { ...prev, x, y } : prev));
  };

  const clearTooltip = () => {
    setHovered(null);
    setTooltip(null);
  };

  const calloutLayout = useMemo(() => {
    const base = processed.segments.map((s, idx) => {
      const connectorStart = toXY(radius + strokeWidth / 2, s.midAngle);
      const radialPoint = toXY(radius + strokeWidth / 2 + connectorLead, s.midAngle);
      const direction = radialPoint.x >= 0 ? 1 : -1;
      const columnX = direction === 1 ? labelColumnOffset : -labelColumnOffset;
      return {
        idx,
        direction: direction as 1 | -1,
        connectorStart,
        radialPoint,
        labelX: columnX,
        baseY: radialPoint.y,
        adjustedY: radialPoint.y,
      };
    });

    const adjustSide = (items: typeof base) => {
      const sorted = [...items].sort((a, b) => a.baseY - b.baseY);
      let prevY = -Infinity;
      return sorted.map((item) => {
        const adjustedY = Math.max(item.baseY, prevY + minLabelGap);
        prevY = adjustedY;
        return { ...item, adjustedY };
      });
    };

    const left = adjustSide(base.filter((it) => it.direction === -1));
    const right = adjustSide(base.filter((it) => it.direction === 1));
    const map = new Map<number, CalloutLayout>();
    [...left, ...right].forEach((item) => map.set(item.idx, item as CalloutLayout));
    return map;
  }, [processed, radius, strokeWidth, labelColumnOffset]);

  return (
    <div className={`relative ${className}`} ref={containerRef}>
      <svg
        width={canvasSize}
        height={canvasSize}
        viewBox={`0 0 ${canvasSize} ${canvasSize}`}
        role="img"
        aria-label="donut chart"
        style={{ overflow: "visible" }}
      >
        <g transform={`translate(${canvasSize / 2}, ${canvasSize / 2})`}>
          {/* background ring */}
          <circle
            r={radius}
            fill="transparent"
            stroke="#E5E7EB"
            strokeWidth={strokeWidth}
          />
          {/* segments */}
          {processed.segments.map((s, idx) => (
            <circle
              key={idx}
              r={radius}
              fill="transparent"
              stroke={s.color}
              strokeWidth={hovered === idx ? strokeWidth + hoverGrow : strokeWidth}
              strokeDasharray={s.dasharray}
              strokeDashoffset={s.dashoffset}
              strokeLinecap="butt"
              style={{
                opacity:
                  (selectedIndex === null || selectedIndex === idx) && (hovered === null || hovered === idx)
                    ? 1
                    : 0.3,
                transition: "opacity 120ms ease, stroke-width 120ms ease",
              }}
              transform={`rotate(${(-START_ANGLE * 180) / Math.PI})`} // start offset
            >
              <title>
                {s.label}: {s.value}
              </title>
            </circle>
          ))}
          {/* interactive wedges for bigger hit-area */}
          {processed.segments.map((s, idx) => (
            <path
              key={`hit-${idx}`}
              d={arcPath(s.startAngle, s.endAngle, outerR + hoverGrow / 2)}
              fill="transparent"
              stroke="transparent"
              strokeWidth={1}
              style={{ cursor: "pointer" }}
              onClick={() => setSelectedIndex((cur) => (cur === idx ? null : idx))}
              onMouseEnter={(e) => showTooltip(idx, e)}
              onMouseMove={moveTooltip}
              onMouseLeave={clearTooltip}
            />
          ))}
          {/* callout labels */}
          {processed.segments.map((s, idx) => {
            const layout = calloutLayout.get(idx);
            if (!layout) {
              return null;
            }
            const { direction, connectorStart, radialPoint, labelX, adjustedY } = layout;
            const textX = labelX + direction * labelOffset;
            const labelY = adjustedY;
            const percent = Math.round((s.frac || 0) * 100);
            const active =
              (selectedIndex === null || selectedIndex === idx) && (hovered === null || hovered === idx);
            const connectorEnd = { x: labelX, y: adjustedY };
            const polylinePoints = `${connectorStart.x},${connectorStart.y} ${radialPoint.x},${radialPoint.y} ${connectorEnd.x},${connectorEnd.y}`;
            return (
              <g
                key={`callout-${idx}`}
                style={{ opacity: active ? 1 : 0.35, cursor: "pointer" }}
                onMouseEnter={(e) => showTooltip(idx, e)}
                onMouseMove={moveTooltip}
                onMouseLeave={clearTooltip}
                onClick={() => setSelectedIndex((cur) => (cur === idx ? null : idx))}
              >
                <polyline
                  points={polylinePoints}
                  fill="none"
                  stroke="#9ca3af"
                  strokeWidth={1}
                />
                <circle cx={connectorEnd.x} cy={adjustedY} r={2} fill={s.color} />
                <text
                  x={textX}
                  y={labelY}
                  textAnchor={direction === 1 ? "start" : "end"}
                  style={{ fontSize: 12, fontWeight: 500, dominantBaseline: "middle" }}
                  fill="#111827"
                >
                  {s.label} <tspan style={{ fontSize: 11, fill: "#6b7280", fontWeight: 400 }}>· {percent}%</tspan>
                </text>
              </g>
            );
          })}
          {/* center labels */}
          <g>
            {centerLabel && (
              <text
                x={0}
                y={-4}
                textAnchor="middle"
                className="fill-current"
                style={{ fontSize: 16, fontWeight: 600 }}
              >
                {hovered !== null ? processed.segments[hovered]?.label : centerLabel}
              </text>
            )}
            {centerSubLabel && (
              <text
                x={0}
                y={14}
                textAnchor="middle"
                className="fill-current"
                style={{ fontSize: 11, opacity: 0.7 }}
              >
                {hovered !== null
                  ? `${valueFormatter ? valueFormatter(processed.segments[hovered]!.value) : processed.segments[hovered]!.value} · ${Math.round((processed.segments[hovered]?.frac || 0) * 100)}%`
                  : centerSubLabel}
              </text>
            )}
          </g>
        </g>
      </svg>
      {tooltip && (
        <div
          className="pointer-events-none absolute z-10 px-2 py-1 rounded bg-foreground text-background text-xs shadow"
          style={{ left: tooltip.x + 8, top: tooltip.y - 8 }}
        >
          {tooltip.text}
        </div>
      )}
    </div>
  );
}


