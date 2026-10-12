import axios from "axios";

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
  withCredentials: true, // send the session cookie (Discord login)
  // The backend requires this header on cookie-authenticated writes (CSRF
  // protection): only our own frontend is allowed to send it.
  headers: { "X-Requested-With": "dashboard" },
});

api.interceptors.request.use((config) => {
  const key = localStorage.getItem("apiKey");
  if (key) config.headers["X-API-Key"] = key;
  return config;
});

export default api;
