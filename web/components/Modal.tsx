"use client";

import { useEffect, useId, useRef } from "react";
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
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useEffect(() => {
    if (!open) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const dialog = dialogRef.current;

    const focusableSelector =
      'a[href], button:not([disabled]), textarea, input, select, [tabindex]:not([tabindex="-1"])';

    // Move initial focus into the dialog.
    const focusables = dialog?.querySelectorAll<HTMLElement>(focusableSelector);
    (focusables && focusables.length ? focusables[0] : dialog)?.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (e.key === "Tab" && dialog) {
        const nodes = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector)).filter(
          (el) => el.offsetParent !== null
        );
        if (nodes.length === 0) {
          e.preventDefault();
          return;
        }
        const first = nodes[0];
        const last = nodes[nodes.length - 1];
        const active = document.activeElement;
        if (e.shiftKey && active === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && active === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };

    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
      previouslyFocused?.focus?.();
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-3">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm animate-in" onClick={onClose} />
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        tabIndex={-1}
        className={`relative w-full ${maxWidthClass} rounded-lg bg-card text-foreground border border-border shadow-2xl overflow-hidden animate-in`}
      >
        <div className="px-4 sm:px-5 py-3 border-b border-border flex items-center justify-between bg-card">
          <h3 id={titleId} className="text-lg font-semibold">{title}</h3>
          <button
            onClick={onClose}
            aria-label="Close"
            className="inline-flex items-center justify-center w-8 h-8 rounded-lg text-muted-foreground hover:text-foreground hover:bg-secondary/40"
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


