import { useState } from "react";
import { getApiUrl } from "../config";

function initials(name) {
    if (!name) return "?";
    const words = name.trim().split(/\s+/);
    if (words.length === 1) return words[0].substring(0, 2).toUpperCase();
    return words.map((w) => w[0]).join("").toUpperCase();
}

function buildLogoSrc(logoUrl) {
    if (!logoUrl) return "";
    const base = String(logoUrl);
    return base.startsWith("http://") || base.startsWith("https://")
        ? base
        : `${getApiUrl()}${base}`;
}

export default function TeamLogo({
    name,
    logoUrl,
    size = 40,
    className = "",
    style = {},
}) {
    const [imgFailed, setImgFailed] = useState(false);
    const src = logoUrl ? buildLogoSrc(logoUrl) : "";

    const showImage = src && !imgFailed;

    if (showImage) {
        return (
            <img
                src={src}
                alt={`${name} logo`}
                className={`rounded border border-secondary ${className}`}
                style={{ width: size, height: size, objectFit: "cover", ...style }}
                onError={() => setImgFailed(true)}
                loading="lazy"
            />
        );
    }

    const abbr = initials(name || "?");

    return (
        <div
            className={`rounded border border-secondary d-flex align-items-center justify-content-center fw-bold ${className}`}
            style={{
                width: size,
                height: size,
                backgroundColor: "#374151",
                color: "#D1D5DB",
                fontSize: Math.max(10, size * 0.36),
                flexShrink: 0,
                ...style,
            }}
            title={name}
        >
            {abbr}
        </div>
    );
}
