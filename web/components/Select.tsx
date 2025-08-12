"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";

type OptionValue = string | number;

export interface SelectOption {
  value: OptionValue;
  label: string;
}

interface SelectProps {
  value: OptionValue;
  onChange: (value: OptionValue) => void;
  options: SelectOption[];
  className?: string;
  size?: "sm" | "md";
  placeholder?: string;
}

export default function Select({ value, onChange, options, className = "", size = "md", placeholder }: SelectProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const selected = useMemo(() => options.find((o) => String(o.value) === String(value)), [options, value]);

  const close = useCallback(() => setOpen(false), []);
  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!containerRef.current) return;
      if (!containerRef.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, []);

  return (
    <div className={`relative ${className}`} ref={containerRef}>
      <button
        ref={buttonRef}
        type="button"
        className={`input ${size === "sm" ? "h-8 text-xs" : "h-10 text-sm"} w-full text-left flex items-center justify-between`}
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="truncate">{selected?.label ?? placeholder ?? ""}</span>
      </button>
      {open && (
        <div
          className="absolute z-20 mt-2 w-full rounded-md border bg-background shadow-md overflow-auto max-h-60 animate-in"
          role="listbox"
        >
          {options.map((opt) => {
            const isSelected = String(opt.value) === String(value);
            return (
              <button
                key={String(opt.value)}
                type="button"
                className={`w-full text-left px-3 py-2 text-sm hover:bg-accent ${isSelected ? "bg-accent" : ""}`}
                onClick={() => {
                  onChange(opt.value);
                  close();
                }}
                role="option"
                aria-selected={isSelected}
              >
                <span className="truncate block">{opt.label}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}


