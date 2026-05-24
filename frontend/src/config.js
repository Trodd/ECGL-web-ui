export function getApiUrl() {
    return (
        (window.__RUNTIME_CONFIG__ && window.__RUNTIME_CONFIG__.API_URL) ||
        import.meta.env.VITE_API_URL ||
        "http://localhost:8080"
    );
}
