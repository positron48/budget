"use client";

import { useEffect } from "react";
import { createPortal } from "react-dom";
import Icon from "./Icon";

interface ModalProps {
  open: boolean;
  title?: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  maxWidthClass?: string; // e.g. max-w-lg
}

export default function Modal({ open, title, onClose, children, footer, maxWidthClass = "max-w-lg" }: ModalProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-3">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm animate-in" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        className={`relative w-full ${maxWidthClass} rounded-none bg-card text-foreground border border-border shadow-2xl overflow-hidden animate-in`}
      >
        <div className="px-4 sm:px-5 py-3 border-b border-border flex items-center justify-between bg-card">
          <h3 className="text-lg font-semibold">{title}</h3>
          <button
            onClick={onClose}
            aria-label="Close"
            className="inline-flex items-center justify-center w-8 h-8 rounded-none text-muted-foreground hover:text-foreground hover:bg-secondary/40"
          >
            <Icon name="close" size={16} />
          </button>
        </div>
        <div className="px-4 sm:px-5 py-3 max-h-[75vh] overflow-y-auto">
          {children}
        </div>
        {footer && (
          <div className="px-4 sm:px-5 py-3 border-t border-border bg-card">
            <div className="flex items-center justify-end gap-2">{footer}</div>
          </div>
        )}
      </div>
    </div>,
    document.body
  );
}


