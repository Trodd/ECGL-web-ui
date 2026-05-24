import { useEffect, useState } from "react";
import axios from "axios";
import { getApiUrl } from "../config";

export default function DiscordRequiredModal({ show, onClose }) {
    const [info, setInfo] = useState(null);
    const urlBase = getApiUrl();

    useEffect(() => {
        if (!show) return;

        axios.get(`${urlBase}/api/discord/server-info`)
            .then(res => setInfo(res.data))
            .catch(() => setInfo(null));
    }, [show]);

    if (!show) return null;

    const invite = info?.invite || import.meta.env.VITE_DISCORD_INVITE;

    return (
        <div className="modal-overlay">
            <div className="modal-card">

                {/* Banner */}
                {info?.banner && (
                    <div
                        className="discord-banner"
                        style={{ backgroundImage: `url(${info.banner})` }}
                    />
                )}

                <div className="discord-invite">
                    <img src={info?.icon} className="server-icon" />

                    <div className="server-info">
                        <h3 className="server-name">{info?.name || "Discord Server"}</h3>

                        <div className="server-stats">
                            <span className="online-dot"></span>
                            {info?.online ?? "—"} Online • {info?.members ?? "—"} Members
                        </div>

                        <p className="join-desc">You must join our Discord server to register.</p>

                        <a href={invite} target="_blank" rel="noreferrer">
                            <button className="join-btn">Join</button>
                        </a>
                    </div>
                </div>

                <button className="close-btn" onClick={onClose}>✖</button>
            </div>
        </div>
    );
}
