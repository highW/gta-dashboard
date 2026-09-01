import { useState } from "react";
import { useAuth } from "../context/AuthContext";

export default function Login() {
    const { login } = useAuth();
    const [key, setKey] = useState("");
    const [error, setError] = useState("");
    const [submitting, setSubmitting] = useState(false);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setSubmitting(true);
        setError("");
        const ok = await login(key);
        if (!ok) setError("Invalid API key");
        setSubmitting(false);
    };

    return (
        <div className="min-h-screen flex items-center justify-center bg-gray-950 text-gray-100">
            <form
                onSubmit={handleSubmit}
                className="bg-gray-900 border border-gray-800 rounded-lg p-8 space-y-4 w-80"
            >
                <h1 className="text-xl font-bold text-center">GTA Server Dashboard</h1>
                <input
                    type="password"
                    placeholder="API Key"
                    value={key}
                    onChange={(e) => setKey(e.target.value)}
                    className="w-full bg-gray-950 border border-gray-800 rounded-md px-3 py-2 text-sm focus:outline-none focus:border-gray-600"
                />
                {error && <p className="text-red-400 text-sm">{error}</p>}
                <button
                    type="submit"
                    disabled={submitting}
                    className="w-full px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-md text-sm"
                >
                    {submitting ? "Checking..." : "Sign In"}
                </button>
            </form>
        </div>
    );
}
