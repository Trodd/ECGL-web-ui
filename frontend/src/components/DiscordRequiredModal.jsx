import { useEffect, useState } from "react";
import axios from "axios";

export default function DiscordRequiredModal({ show, onClose }) {
    const [serverName, setServerName] = useState("ECGL Server");
    const [serverIcon, setServerIcon] = useState(null);
    const [serverBanner, setServerBanner] = useState(null);
    const [invite, setInvite] = useState("#");

    const api = import.meta.env.VITE_API_URL;

    useEffect(() => {
        if (!show) return;

        axios.get(`${api}/api/discord/info`)
            .then(res => {
                const guild = res.data || {};

                setServerName(guild.name || "ECGL Server");

                if (guild.icon) {
                    setServerIcon(
                        `https://cdn.discordapp.com/icons/${guild.id}/${guild.icon}.png?size=128`
                    );
                }

                if (guild.banner) {
                    setServerBanner(
                        `https://cdn.discordapp.com/banners/${guild.id}/${guild.banner}.png?size=2048`
                    );
                }

                if (guild.invite) {
                    setInvite(guild.invite);
                }
            });
    }, [show]);

    if (!show) return null;

    return (
        <div className="discord-overlay">
            <div className="discord-window bg-dark text-light rounded shadow-lg">

                <div className="p-4">
                    <h3 className="text-warning text-center mb-3">🚫 Join the ECGL Discord</h3>

                    <p className="text-center mb-4">
                        You must join the ECGL Discord server before registering.
                    </p>

                    {/* Invite Card */}
                    <div className="invite-card p-3 rounded border border-secondary mb-3 bg-black">
                        <div className="d-flex align-items-center mb-3">
                            <img
                                src={serverIcon || "/default-discord-icon.png"}
                                alt="Server Icon"
                                style={{
                                    width: 60,
                                    height: 60,
                                    borderRadius: 12,
                                    marginRight: 12
                                }}
                            />

                            <div>
                                <h5 className="text-light mb-1">{serverName}</h5>
                                <span className="text-success small">Official ECGL Server</span>
                            </div>
                        </div>

                        <a
                            href={invite}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="btn btn-primary w-100 fw-bold"
                        >
                            Join Discord Server
                        </a>
                    </div>

                    <button className="btn btn-secondary w-100" onClick={onClose}>
                        Close
                    </button>
                </div>
            </div>

            <style>{`
                .discord-overlay {
                    position: fixed;
                    inset: 0;
                    background: rgba(0,0,0,0.65);
                    display: flex;
                    justify-content: center;
                    align-items: center;
                    z-index: 9999;
                }
                .discord-window {
                    width: 420px;
                    max-width: 95%;
                    overflow: hidden;
                }
                .banner-wrapper {
                    width: 100%;
                    height: 120px;
                    overflow: hidden;
                }
                .banner-image {
                    width: 100%;
                    height: 100%;
                    object-fit: cover;
                }
            `}</style>
        </div>
    );
}
