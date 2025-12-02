"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

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
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropdownPos, setDropdownPos] = useState({ top: 0, left: 0, width: 0 });

  const selected = useMemo(() => options.find((o) => String(o.value) === String(value)), [options, value]);

  const close = useCallback(() => setOpen(false), []);
  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!containerRef.current) return;
      const target = e.target as Node;
      if (
        !containerRef.current.contains(target) &&
        !(dropdownRef.current && dropdownRef.current.contains(target))
      ) {
        setOpen(false);
      }
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

  const updateDropdownPosition = useCallback(() => {
    if (!buttonRef.current) return;
    const rect = buttonRef.current.getBoundingClientRect();
    setDropdownPos({
      top: rect.bottom + window.scrollY + 4,
      left: rect.left + window.scrollX,
      width: rect.width,
    });
  }, []);

  useEffect(() => {
    if (!open) return;
    updateDropdownPosition();
    const handleScroll = () => updateDropdownPosition();
    window.addEventListener("scroll", handleScroll, true);
    window.addEventListener("resize", handleScroll);
    return () => {
      window.removeEventListener("scroll", handleScroll, true);
      window.removeEventListener("resize", handleScroll);
    };
  }, [open, updateDropdownPosition]);

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
      {open &&
        createPortal(
          <div
            ref={dropdownRef}
            className="fixed z-50 rounded-none border border-border text-foreground shadow-xl overflow-auto max-h-60 animate-in backdrop-blur supports-[backdrop-filter]:bg-card/70"
            style={{
              backgroundColor: "hsl(var(--card) / 0.95)",
              top: dropdownPos.top,
              left: dropdownPos.left,
              width: dropdownPos.width,
            }}
            role="listbox"
          >
            {options.map((opt) => {
              const isSelected = String(opt.value) === String(value);
              return (
                <button
                  key={String(opt.value)}
                  type="button"
                  className={`w-full text-left px-3 py-2 text-sm transition-colors ${
                    isSelected ? "bg-secondary/60 text-foreground" : "text-foreground hover:bg-secondary/40"
                  }`}
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
          </div>,
          document.body
        )}
    </div>
  );
}


