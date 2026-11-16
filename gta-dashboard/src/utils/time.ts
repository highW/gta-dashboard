/**
 * How long ago something happened: "just now", "5 min ago", "3 h ago", "12 d ago".
 * The API sends times as UTC ("2026-11-16T21:04:05Z"); Date handles the
 * conversion to the viewer's own time zone.
 */
export function timeAgo(ts: string | number | Date): string {
  const then = new Date(ts).getTime();
  if (Number.isNaN(then)) return "unknown";

  const secs = Math.max(0, (Date.now() - then) / 1000);
  if (secs < 60) return "just now";
  if (secs < 3600) return `${Math.floor(secs / 60)} min ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)} h ago`;
  return `${Math.floor(secs / 86400)} d ago`;
}
