/**
 * toast — tiny site-wide notification bus.
 *
 * Usage:
 *   toast("✅ Saved!");                 // type inferred from the text
 *   toast("Something broke", "danger");  // explicit type
 *
 * Anything that used to be an alert() or an inline banner should call this
 * instead; <ToastHost /> (mounted once in App) renders the stack.
 */

const listeners = new Set();
let nextId = 1;

const SUCCESS_RE = /✅|\bsuccess|saved|sent|updated|assigned|complete|completed|added|created|removed|deleted|moved/i;
const DANGER_RE = /❌|\bfailed|failure|\berror\b|could not|cannot|not found|unauthorized|forbidden/i;
const WARNING_RE = /⚠️|\bwarning\b|closed|disabled|locked|already|cooldown|full\b/i;

function inferType(text) {
  if (DANGER_RE.test(text)) return "danger";
  if (WARNING_RE.test(text)) return "warning";
  if (SUCCESS_RE.test(text)) return "success";
  return "info";
}

/**
 * @param {string} message
 * @param {"success"|"danger"|"warning"|"info"} [type]
 * @param {{ duration?: number, link?: string }} [opts]
 */
export function toast(message, type, opts = {}) {
  const text = String(message ?? "").trim();
  if (!text) return;

  const kind = type || inferType(text);
  const item = {
    id: nextId++,
    text,
    type: kind,
    link: opts.link || "",
    duration: opts.duration ?? (kind === "danger" ? 6000 : 4000),
  };

  listeners.forEach((fn) => {
    try {
      fn(item);
    } catch {
      /* ignore listener errors */
    }
  });
}

export function subscribeToasts(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Shortcut helpers. */
export const toastSuccess = (m, o) => toast(m, "success", o);
export const toastError = (m, o) => toast(m, "danger", o);
export const toastWarning = (m, o) => toast(m, "warning", o);
export const toastInfo = (m, o) => toast(m, "info", o);
