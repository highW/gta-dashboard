import axios from "axios";
import type { InternalAxiosRequestConfig } from "axios";

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

// The server allows each client a limited burst of requests. A 429 answer
// means the request was turned away before it did anything, so it is safe
// to wait as long as the server asks (Retry-After) and send it again. After
// two retries the error is passed on like any other.
const MAX_RATE_LIMIT_RETRIES = 2;
const MAX_RETRY_WAIT_SECONDS = 5;

type RetriableConfig = InternalAxiosRequestConfig & { rateLimitRetries?: number };

api.interceptors.response.use(undefined, async (error: unknown) => {
  if (!axios.isAxiosError(error) || error.response?.status !== 429 || !error.config) {
    throw error;
  }
  const config: RetriableConfig = error.config;
  const retries = config.rateLimitRetries ?? 0;
  if (retries >= MAX_RATE_LIMIT_RETRIES) throw error;
  config.rateLimitRetries = retries + 1;

  const asked = Number(error.response.headers["retry-after"]);
  const seconds = asked > 0 ? Math.min(asked, MAX_RETRY_WAIT_SECONDS) : 1;
  await new Promise((resolve) => setTimeout(resolve, seconds * 1000));
  return api.request(config);
});

export default api;
