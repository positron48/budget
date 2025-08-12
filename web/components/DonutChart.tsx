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
}

export default function DonutChart({
  data,
  size = 220,
  strokeWidth = 22,
  centerLabel,
  centerSubLabel,
  className = "",
}: DonutChartProps) {
  const hoverGrow = 6;
  const margin = 6;
  const maxStroke = strokeWidth + hoverGrow;
  const radius = (size - maxStroke - margin * 2) / 2;
  const circumference = 2 * Math.PI * radius;
  const containerRef = useRef<HTMLDivElement>(null);
  const [hovered, setHovered] = useState<number | null>(null);
  const [tooltip, setTooltip] = useState<{ x: number; y: number; text: string } | null>(null);
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null);

  const palette = [
    "#6366f1", // indigo-500
    "#22c55e", // green-500
    "#ef4444", // red-500
    "#06b6d4", // cyan-500
    "#f59e0b", // amber-500
    "#a855f7", // purple-500
    "#14b8a6", // teal-500
    "#3b82f6", // blue-500
    "#f97316", // orange-500
  ];

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
      };
      offset += length;
      startAngle = endAngle;
      return seg;
    });
    return { total, segments };
  }, [data, circumference]);

  const outerR = radius + strokeWidth / 2; // outer bound for wedge
  const toXY = (r: number, angle: number) => {
    return { x: Math.cos(angle - Math.PI / 2) * r, y: Math.sin(angle - Math.PI / 2) * r };
  };
  const arcPath = (a0: number, a1: number, r: number) => {
    const p0 = toXY(r, a0);
    const p1 = toXY(r, a1);
    const large = a1 - a0 > Math.PI ? 1 : 0;
    return `M 0 0 L ${p0.x.toFixed(3)} ${p0.y.toFixed(3)} A ${r} ${r} 0 ${large} 1 ${p1.x.toFixed(3)} ${p1.y.toFixed(3)} Z`;
  };

  return (
    <div className={`relative ${className}`} ref={containerRef}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label="donut chart">
        <g transform={`translate(${size / 2}, ${size / 2})`}>
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
              transform="rotate(-90)" // start at top
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
              onMouseEnter={(e) => {
                setHovered(idx);
                const rect = containerRef.current?.getBoundingClientRect();
                const x = e.clientX - (rect?.left || 0);
                const y = e.clientY - (rect?.top || 0);
                setTooltip({ x, y, text: `${s.label}: ${s.value} (${Math.round(s.frac * 100)}%)` });
              }}
              onMouseMove={(e) => {
                if (!tooltip || hovered !== idx) return;
                const rect = containerRef.current?.getBoundingClientRect();
                const x = e.clientX - (rect?.left || 0);
                const y = e.clientY - (rect?.top || 0);
                setTooltip({ ...tooltip, x, y });
              }}
              onMouseLeave={() => {
                setHovered(null);
                setTooltip(null);
              }}
            />
          ))}
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
                  ? `${processed.segments[hovered]?.value} · ${Math.round((processed.segments[hovered]?.frac || 0) * 100)}%`
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
      {/* legend */}
      <div className="mt-3 grid grid-cols-1 gap-1 text-sm">
        {processed.segments.map((s, idx) => (
          <button
            key={idx}
            type="button"
            className="flex items-center gap-2 text-left"
            onMouseEnter={() => setHovered(idx)}
            onMouseLeave={() => setHovered(null)}
            onClick={() => setSelectedIndex((cur) => (cur === idx ? null : idx))}
          >
            <span
              className="inline-block w-3 h-3 rounded"
              style={{ backgroundColor: s.color, opacity: selectedIndex === null || selectedIndex === idx ? 1 : 0.35 }}
            />
            <span
              className="truncate"
              title={s.label}
              style={{ opacity: selectedIndex === null || selectedIndex === idx ? 1 : 0.5 }}
            >
              {s.label}
            </span>
            <span
              className="ml-auto tabular-nums"
              title={String(s.value)}
              style={{ opacity: selectedIndex === null || selectedIndex === idx ? 1 : 0.5 }}
            >
              {Math.round(s.frac * 100)}%
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}


