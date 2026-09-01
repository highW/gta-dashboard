export default function PlayerListSkeleton() {
    return (
        <div className="rounded-lg border border-[var(--border-color)] overflow-hidden animate-pulse">
            <div className="bg-[var(--bg-surface)] px-4 py-3">
                <div className="h-3 w-24 bg-[var(--border-color)] rounded" />
            </div>
            {[...Array(5)].map((_, i) => (
                <div
                    key={i}
                    className="flex items-center gap-6 px-4 py-3 border-t border-[var(--border-color)]"
                >
                    <div className="h-3 w-32 bg-[var(--border-color)] rounded" />
                    <div className="h-3 w-12 bg-[var(--border-color)] rounded" />
                    <div className="h-3 w-16 bg-[var(--border-color)] rounded" />
                </div>
            ))}
        </div>
    );
}
