import axios from "axios";

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
});

api.interceptors.request.use((config)=>{
    const key = localStorage.getItem("apiKey");
    if (key) config.headers["X-API-Key"] = key;
    return config;
})

export default api;