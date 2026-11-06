import { useEffect, useState } from "react";
import { subscribeToasts } from "../toast";

const ICONS = {
  success: "✅",
  danger: "❌",
  warning: "⚠️",
  info: "ℹ️",
};

const COLORS = {
  success: { border: "#2f9e44", bg: "#12281a", text: "#d3f9d8" },
  danger: { border: "#e03131", bg: "#2a1414", text: "#ffe3e3" },
  warning: { border: "#e8a33d", bg: "#2a2213", text: "#ffe8cc" },
  info: { border: "#4dabf7", bg: "#131f2a", text: "#d0ebff" },
};

export default function ToastHost() {
  const [items, setItems] = useState([]);

  // Mark an item as leaving so it can animate out, then drop it.
  function dismiss(id) {
    setItems((prev) =>
      prev.map((t) => (t.id === id ? { ...t, leaving: true } : t))
    );
    setTimeout(() => {
      setItems((prev) => prev.filter((t) => t.id !== id));
    }, 240);
  }

  useEffect(() => {
    return subscribeToasts((item) => {
      setItems((prev) => [...prev.slice(-4), item]); // keep at most 5 on screen
      if (item.duration > 0) {
        setTimeout(() => dismiss(item.id), item.duration);
      }
    });
  }, []);

  if (items.length === 0) return null;

  return (
    <div className="toast-host">
      {items.map((t) => {
        const c = COLORS[t.type] || COLORS.info;
        return (
          <div
            key={t.id}
            className={`toast-item${t.leaving ? " leaving" : ""}`}
            onClick={() => {
              dismiss(t.id);
              if (t.link) window.location.href = t.link;
            }}
            role="status"
            style={{ borderLeftColor: c.border, background: c.bg, color: c.text }}
          >
            <span className="toast-icon">{ICONS[t.type] || ICONS.info}</span>
            <span className="toast-text">{t.text}</span>
            {t.link && <span className="toast-open">Open →</span>}
            <button
              type="button"
              className="toast-close"
              aria-label="Dismiss"
              onClick={(e) => {
                e.stopPropagation();
                dismiss(t.id);
              }}
            >
              ✕
            </button>
          </div>
        );
      })}

      <style>{`
      .toast-host {
        position: fixed;
        right: 20px;
        bottom: 20px;
        z-index: 20000;
        display: flex;
        flex-direction: column;
        align-items: flex-end;
        gap: 10px;
        width: min(94vw, 460px);
        pointer-events: none;
        overflow: hidden;
      }
      .toast-item {
        pointer-events: auto;
        display: flex;
        align-items: flex-start;
        gap: 10px;
        width: 100%;
        padding: 14px 16px;
        border-radius: 10px;
        border: 1px solid rgba(255,255,255,0.14);
        border-left: 6px solid #4dabf7;
        box-shadow: 0 14px 34px rgba(0,0,0,0.6);
        font-size: 0.98rem;
        line-height: 1.35;
        cursor: pointer;
        animation: toast-in .24s cubic-bezier(.22,.8,.3,1);
      }
      .toast-item.leaving {
        animation: toast-out .22s ease-in forwards;
      }
      .toast-icon { flex: 0 0 auto; line-height: 1.25; font-size: 1.05rem; }
      .toast-text { flex: 1 1 auto; white-space: pre-wrap; word-break: break-word; }
      .toast-close {
        flex: 0 0 auto;
        background: none;
        border: none;
        color: inherit;
        opacity: .6;
        font-size: .95rem;
        padding: 0 2px;
        cursor: pointer;
      }
      .toast-close:hover { opacity: 1; }
      .toast-open {
        flex: 0 0 auto;
        align-self: center;
        font-size: 0.78rem;
        font-weight: 700;
        letter-spacing: .3px;
        text-transform: uppercase;
        opacity: .85;
        white-space: nowrap;
      }
      @keyframes toast-in {
        from { opacity: 0; transform: translateX(120%); }
        to   { opacity: 1; transform: translateX(0); }
      }
      @keyframes toast-out {
        from { opacity: 1; transform: translateX(0); }
        to   { opacity: 0; transform: translateX(120%); }
      }
      @media (max-width: 576px) {
        .toast-host {
          left: 12px;
          right: 12px;
          bottom: 12px;
          width: auto;
        }
        .toast-item { font-size: 0.92rem; padding: 12px 14px; }
      }
      @media (prefers-reduced-motion: reduce) {
        .toast-item,
        .toast-item.leaving { animation-duration: .01ms; }
      }
      `}</style>
    </div>
  );
}
