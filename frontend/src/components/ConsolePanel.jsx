import { useEffect, useRef, useState } from "react";
import axios from "axios";

export default function ConsolePanel({ urlBase }) {
    const [logs, setLogs] = useState([]);
    const [command, setCommand] = useState("");
    const bottomRef = useRef(null);

    // Auto-scroll on new output
    useEffect(() => {
        bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [logs]);

    useEffect(() => {
        if (!urlBase) return;

        let cancelled = false;

        axios
            .get(`${urlBase}/api/mod/console/welcome`, {
                withCredentials: true,
            })
            .then((res) => {
                if (cancelled) return;

                if (Array.isArray(res.data?.lines)) {
                    setLogs((l) => [
                        ...l,
                        ...res.data.lines.map((t) => ({
                            level: "INFO",
                            text: t,
                        })),
                    ]);
                }
            })
            .catch(() => {
                // silent fail — console still usable
            });

        return () => {
            cancelled = true;
        };
    }, [urlBase]);

    async function runCommand(e) {
        e.preventDefault();
        if (!command.trim() || !urlBase) return;

        const cmd = command.trim();
        setCommand("");

        // Echo command (Minecraft style)
        setLogs((l) => [
            ...l.slice(-199),
            { level: "CMD", text: `> ${cmd}` },
        ]);

        try {
            const res = await axios.post(
                `${urlBase}/api/mod/console`,
                { command: cmd },
                { withCredentials: true }
            );

            if (res.data?.lines?.length) {
                setLogs((l) => [
                    ...l.slice(-180),
                    ...res.data.lines.map((t) => ({
                        level: "INFO",
                        text: t,
                    })),
                ]);
            } else {
                setLogs((l) => [
                    ...l.slice(-199),
                    { level: "INFO", text: "OK" },
                ]);
            }
        } catch (err) {
            setLogs((l) => [
                ...l.slice(-199),
                {
                    level: "ERROR",
                    text:
                        err.response?.data?.error ||
                        err.response?.data ||
                        "Command failed",
                },
            ]);
        }
    }

    const colorFor = (level) => {
        switch (level) {
            case "ERROR": return "#ff5555";
            case "WARN": return "#ffaa00";
            case "CMD": return "#55ffff";
            default: return "#aaffaa";
        }
    };

    return (
        <div style={styles.wrapper}>
            <div style={styles.header}>🖥️ ECGL Server Console</div>

            <div style={styles.console}>
                {logs.length === 0 && (
                    <div style={{ color: "#777" }}>
                        Console ready. Type <b>help</b>.
                    </div>
                )}

                {logs.map((l, i) => (
                    <div key={i} style={{ color: colorFor(l.level) }}>
                        <span style={{ opacity: 0.6 }}>
                            [{l.level}]
                        </span>{" "}
                        {l.text}
                    </div>
                ))}

                <div ref={bottomRef} />
            </div>

            <form onSubmit={runCommand} style={styles.inputRow}>
                <span style={{ color: "#55ffff" }}>{">"}</span>
                <input
                    value={command}
                    onChange={(e) => setCommand(e.target.value)}
                    placeholder="Enter command..."
                    style={styles.input}
                    autoComplete="off"
                />
            </form>
        </div>
    );
}

const styles = {
    wrapper: {
        background: "#111",
        border: "1px solid #333",
        borderRadius: 8,
        overflow: "hidden",
        fontFamily: "monospace",
    },
    header: {
        background: "#000",
        padding: "6px 10px",
        borderBottom: "1px solid #333",
        color: "#9ecbff",
        fontWeight: "bold",
    },
    console: {
        height: 260,
        overflowY: "auto",
        padding: 10,
        fontSize: 13,
        background: "#0a0a0a",
    },
    inputRow: {
        display: "flex",
        alignItems: "center",
        gap: 8,
        padding: 8,
        borderTop: "1px solid #333",
        background: "#000",
    },
    input: {
        flex: 1,
        background: "transparent",
        border: "none",
        outline: "none",
        color: "#fff",
        fontFamily: "monospace",
    },
};
