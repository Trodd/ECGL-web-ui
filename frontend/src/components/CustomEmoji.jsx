import "./custom-emoji.css";

const icons = {
    lightning: (
        <path d="M13 2L4 14h6l-1 8 9-12h-6l1-8z" fill="currentColor" />
    ),
    calendar: (
        <>
            <rect x="3" y="4" width="18" height="17" rx="2" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M7 2v4M17 2v4M3 9h18" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
            <rect x="7" y="12" width="3" height="3" fill="currentColor" />
            <rect x="14" y="12" width="3" height="3" fill="currentColor" />
        </>
    ),
    register: (
        <>
            <path d="M14 3H6a2 2 0 00-2 2v14a2 2 0 002 2h12a2 2 0 002-2V9l-6-6z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M14 3v6h6M8 13h8M8 17h5" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    clipboard: (
        <>
            <path d="M9 2h6v3H9zM7 4H5a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2V6a2 2 0 00-2-2h-2" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M8 11h8M8 15h6" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    trophy: (
        <>
            <path d="M8 21h8M12 17v4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
            <path d="M7 4h10v5a5 5 0 01-10 0V4z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M7 5H4v2a3 3 0 003 3M17 5h3v2a3 3 0 01-3 3" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M10 2h4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    tools: (
        <>
            <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94L6.7 20.3a1.5 1.5 0 01-2.12 0l-.88-.88a1.5 1.5 0 010-2.12l6.83-6.83A6 6 0 0114.7 6.3z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
        </>
    ),
    gear: (
        <>
            <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M10.3 2h3.4l.5 2.4a7 7 0 011.7 1l2.3-.8 1.7 2.9-1.8 1.6a7 7 0 010 2l1.8 1.6-1.7 2.9-2.3-.8a7 7 0 01-1.7 1l-.5 2.4h-3.4l-.5-2.4a7 7 0 01-1.7-1l-2.3.8-1.7-2.9 1.8-1.6a7 7 0 010-2L4.1 8.5l1.7-2.9 2.3.8a7 7 0 011.7-1L10.3 2z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
        </>
    ),
    key: (
        <>
            <circle cx="8" cy="8" r="4" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M11 11l9 9M16 16l2 2M18 14l2 2" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    person: (
        <>
            <circle cx="12" cy="7" r="4" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M5 21v-2a5 5 0 0110 0v2" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="square" />
        </>
    ),
    clock: (
        <>
            <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M12 6v6l4 2" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    swords: (
        <>
            <path d="M5 5l6 6M19 5l-6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
            <path d="M3 3l3 0 0 3M21 3l-3 0 0 3" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M9 13l-4 4-2 4 4-2 4-4M15 13l4 4 2 4-4-2-4-4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" strokeLinejoin="miter" />
        </>
    ),
    plus: (
        <>
            <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M12 8v8M8 12h8" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    trash: (
        <>
            <path d="M4 6h16M9 6V4h6v2M6 6v13a2 2 0 002 2h8a2 2 0 002-2V6" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="square" strokeLinejoin="miter" />
            <path d="M10 10v7M14 10v7" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    bell: (
        <>
            <path d="M12 3a6 6 0 00-6 6v4l-2 3h16l-2-3V9a6 6 0 00-6-6z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M10 20a2 2 0 004 0" stroke="currentColor" strokeWidth="2" fill="none" />
        </>
    ),
    megaphone: (
        <>
            <path d="M18 4L8 8H4v6h4l10 4V4z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M20 8a4 4 0 010 6" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
        </>
    ),
    gamepad: (
        <>
            <rect x="2" y="7" width="20" height="10" rx="4" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M7 10v4M5 12h4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
            <circle cx="16" cy="10" r="1" fill="currentColor" />
            <circle cx="19" cy="12" r="1" fill="currentColor" />
        </>
    ),
    map: (
        <>
            <path d="M3 6l6-2 6 2 6-2v14l-6 2-6-2-6 2V6z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M9 4v14M15 6v14" stroke="currentColor" strokeWidth="2" />
        </>
    ),
    warning: (
        <>
            <path d="M12 3L2 20h20L12 3z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M12 9v4" stroke="currentColor" strokeWidth="2.5" strokeLinecap="square" />
            <circle cx="12" cy="16" r="1" fill="currentColor" />
        </>
    ),
    error: (
        <>
            <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M8 8l8 8M16 8l-8 8" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    check: (
        <>
            <path d="M4 12l5 5L20 6" stroke="currentColor" strokeWidth="2.5" strokeLinecap="square" fill="none" strokeLinejoin="miter" />
        </>
    ),
    banned: (
        <>
            <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M5.5 5.5l13 13" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    refresh: (
        <>
            <path d="M4 12a8 8 0 0114-5.3V4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M20 12a8 8 0 01-14 5.3V20" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M18 4v4h-4M6 20v-4h4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" strokeLinejoin="miter" />
        </>
    ),
    lock: (
        <>
            <rect x="5" y="11" width="14" height="10" rx="2" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M8 11V7a4 4 0 018 0v4" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="square" />
            <circle cx="12" cy="16" r="1.5" fill="currentColor" />
        </>
    ),
    pencil: (
        <>
            <path d="M16 3l5 5-12 12H4v-5L16 3z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M13 6l5 5" stroke="currentColor" strokeWidth="2" />
        </>
    ),
    flag: (
        <>
            <path d="M5 2v20" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
            <path d="M5 4h12l-3 4 3 4H5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
        </>
    ),
    stop: (
        <>
            <path d="M8 2h8l6 6v8l-6 6H8l-6-6V8l6-6z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M8 12h8" stroke="currentColor" strokeWidth="2.5" strokeLinecap="square" />
        </>
    ),
    image: (
        <>
            <rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" strokeWidth="2" fill="none" />
            <circle cx="9" cy="9" r="2" stroke="currentColor" strokeWidth="1.5" fill="none" />
            <path d="M21 15l-5-5-8 8" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
        </>
    ),
    target: (
        <>
            <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <circle cx="12" cy="12" r="5" stroke="currentColor" strokeWidth="2" fill="none" />
            <circle cx="12" cy="12" r="1.5" fill="currentColor" />
        </>
    ),
    shield: (
        <>
            <path d="M12 3L4 7v5c0 5 3.5 8.5 8 10 4.5-1.5 8-5 8-10V7l-8-4z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <path d="M9 12l2 2 4-4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
        </>
    ),
    scales: (
        <>
            <path d="M12 3v18M5 7l7-2 7 2" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M3 13l2-6 2 6a3 3 0 01-4 0zM17 13l2-6 2 6a3 3 0 01-4 0z" stroke="currentColor" strokeWidth="2" fill="none" />
        </>
    ),
    tag: (
        <>
            <path d="M4 4h7l9 9-7 7-9-9V4z" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
            <circle cx="8" cy="8" r="1.5" fill="currentColor" />
        </>
    ),
    timer: (
        <>
            <circle cx="12" cy="13" r="8" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M12 9v4l3 2" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M10 2h4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    pause: (
        <>
            <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M10 8v8M14 8v8" stroke="currentColor" strokeWidth="2.5" strokeLinecap="square" />
        </>
    ),
    medal: (
        <>
            <circle cx="12" cy="14" r="6" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M8 5l4 4 4-4M8 2h8" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" />
            <path d="M12 11v3" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    home: (
        <>
            <path d="M3 12l9-9 9 9" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" strokeLinejoin="miter" />
            <path d="M5 12v7a2 2 0 002 2h10a2 2 0 002-2v-7" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M9 21v-6h6v6" stroke="currentColor" strokeWidth="2" fill="none" strokeLinejoin="miter" />
        </>
    ),
    scroll: (
        <>
            <path d="M8 3h10a2 2 0 012 2v12a4 4 0 01-4 4H8" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M8 3a4 4 0 00-4 4v10a4 4 0 004 4h8" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M10 8h6M10 12h4" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    team: (
        <>
            <circle cx="9" cy="7" r="3" stroke="currentColor" strokeWidth="2" fill="none" />
            <circle cx="17" cy="7" r="2.5" stroke="currentColor" strokeWidth="1.5" fill="none" />
            <path d="M3 20v-2a5 5 0 0110 0v2" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="square" />
            <path d="M15 20v-2a4 4 0 014 0v2" stroke="currentColor" strokeWidth="1.5" fill="none" />
        </>
    ),
    door: (
        <>
            <path d="M5 4h9a2 2 0 012 2v12a2 2 0 01-2 2H5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="square" />
            <path d="M16 12h5M19 9l3 3-3 3" stroke="currentColor" strokeWidth="2" strokeLinecap="square" fill="none" strokeLinejoin="miter" />
            <circle cx="11" cy="12" r="1" fill="currentColor" />
        </>
    ),
    menu: (
        <>
            <path d="M4 6h16M4 12h16M4 18h16" stroke="currentColor" strokeWidth="2.5" strokeLinecap="square" />
        </>
    ),
    signup: (
        <>
            <circle cx="10" cy="7" r="4" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M3 21v-2a6 6 0 016-6h2" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="square" />
            <path d="M17 14v6M14 17h6" stroke="currentColor" strokeWidth="2" strokeLinecap="square" />
        </>
    ),
    leaderboard: (
        <>
            <rect x="3" y="12" width="5" height="9" stroke="currentColor" strokeWidth="2" fill="none" />
            <rect x="9.5" y="5" width="5" height="16" stroke="currentColor" strokeWidth="2" fill="none" />
            <rect x="16" y="9" width="5" height="12" stroke="currentColor" strokeWidth="2" fill="none" />
            <path d="M12 3l-1 2h2l-1-2z" fill="currentColor" />
        </>
    ),
};

export default function CustomEmoji({ name, size, className = "" }) {
    const svg = icons[name];
    if (!svg) return <span className="custom-emoji-fallback">{name}</span>;

    const s = size || "1em";

    return (
        <svg
            className={`custom-emoji ${className}`}
            viewBox="0 0 24 24"
            width={s}
            height={s}
            aria-hidden="true"
        >
            {svg}
        </svg>
    );
}

// Shortcut: inline text-level emoji
export function E({ n, size, className }) {
    return <CustomEmoji name={n} size={size} className={className} />;
}
