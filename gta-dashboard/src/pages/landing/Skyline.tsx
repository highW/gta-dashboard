import { useMemo } from "react";

/**
 * An original night skyline drawn in SVG: three layers of towers with lit
 * windows, a bay, palms, and a police helicopter sweeping its searchlight.
 * Built from a fixed seed, so it looks the same on every load.
 */

// Small deterministic random generator (mulberry32).
function rng(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const W = 1440;
const HORIZON = 372;
const H = 480;

interface Tower { x: number; w: number; h: number; antenna: boolean }
interface Win { x: number; y: number; c: string }

function layer(rand: () => number, minW: number, maxW: number, minH: number, maxH: number, gap: number): Tower[] {
  const towers: Tower[] = [];
  let x = -20;
  while (x < W + 20) {
    const w = minW + rand() * (maxW - minW);
    const tall = rand() < 0.18;
    const h = (tall ? maxH * 0.85 : minH) + rand() * (maxH - minH) * (tall ? 0.15 : 0.6);
    towers.push({ x, w, h, antenna: tall && rand() < 0.6 });
    x += w + rand() * gap;
  }
  return towers;
}

function windows(rand: () => number, towers: Tower[], lit: number, palette: string[]): Win[] {
  const out: Win[] = [];
  for (const t of towers) {
    for (let y = HORIZON - t.h + 10; y < HORIZON - 8; y += 11) {
      for (let x = t.x + 6; x < t.x + t.w - 8; x += 9) {
        if (rand() < lit) out.push({ x, y, c: palette[Math.floor(rand() * palette.length)] });
      }
    }
  }
  return out;
}

function palm(x: number, height: number, lean: number) {
  const topX = x + lean;
  const topY = H - height;
  const trunk = `M ${x} ${H} Q ${x + lean * 0.2} ${H - height * 0.55} ${topX} ${topY}`;
  const fronds = [-150, -120, -95, -60, -30, -5, 25].map((deg, i) => {
    const r = 70 + (i % 3) * 18;
    const a = (deg * Math.PI) / 180;
    const ex = topX + Math.cos(a) * r;
    const ey = topY + Math.sin(a) * r * 0.55 + r * 0.45;
    const cx = topX + Math.cos(a) * r * 0.55;
    const cy = topY + Math.sin(a) * r * 0.6 - 12;
    return `M ${topX} ${topY} Q ${cx} ${cy} ${ex} ${ey}`;
  });
  return { trunk, fronds };
}

export default function Skyline({
  seed = 11,
  helicopter = true,
  className = "",
}: {
  seed?: number;
  helicopter?: boolean;
  className?: string;
}) {
  const scene = useMemo(() => {
    const rand = rng(seed);
    const far = layer(rand, 34, 80, 110, 250, 6);
    const mid = layer(rand, 42, 92, 70, 205, 14);
    const near = layer(rand, 56, 120, 40, 175, 70);
    const stars = Array.from({ length: 60 }, () => ({ x: rand() * W, y: rand() * 170, r: rand() * 1.2 + 0.3 }));
    const midWin = windows(rand, mid, 0.1, ["#ffb54a", "#ff7ab6"]);
    const nearWin = windows(rand, near, 0.2, ["#ffb54a", "#ffd27a", "#ff7ab6", "#7ad7ff"]);
    const ripples = Array.from({ length: 70 }, () => ({
      x: rand() * W,
      y: HORIZON + 8 + rand() * (H - HORIZON - 30),
      w: 8 + rand() * 40,
      c: rand() < 0.6 ? "#ffb54a" : "#ff5d8f",
    }));
    return { far, mid, near, stars, midWin, nearWin, ripples };
  }, [seed]);

  const palms = [palm(70, 330, 34), palm(150, 250, -22), palm(1330, 300, -30), palm(1395, 210, 18)];
  const id = `sky${seed}`;

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="xMidYMax slice"
      className={className}
      role="img"
      aria-label="A city skyline at dusk, with palm trees and a police helicopter"
    >
      <defs>
        <linearGradient id={`${id}-sky`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#0d0a1f" />
          <stop offset="0.35" stopColor="#2a1640" />
          <stop offset="0.62" stopColor="#7a2358" />
          <stop offset="0.8" stopColor="#c4306b" />
          <stop offset="1" stopColor="#f2734a" />
        </linearGradient>
        <linearGradient id={`${id}-bay`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#3a1640" />
          <stop offset="1" stopColor="#07080d" />
        </linearGradient>
        <linearGradient id={`${id}-beam`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#ffffff" stopOpacity="0.55" />
          <stop offset="1" stopColor="#ffffff" stopOpacity="0" />
        </linearGradient>
        <radialGradient id={`${id}-sun`} cx="0.5" cy="0.5" r="0.5">
          <stop offset="0" stopColor="#ffb36b" stopOpacity="0.9" />
          <stop offset="1" stopColor="#ff6a5a" stopOpacity="0" />
        </radialGradient>
      </defs>

      <rect width={W} height={HORIZON} fill={`url(#${id}-sky)`} />
      <ellipse cx={W * 0.52} cy={HORIZON} rx="420" ry="150" fill={`url(#${id}-sun)`} />
      {scene.stars.map((s, i) => (
        <circle key={i} cx={s.x} cy={s.y} r={s.r} fill="#fff" opacity="0.55" />
      ))}

      <g fill="#3a1d4f" opacity="0.85">
        {scene.far.map((t, i) => <rect key={i} x={t.x} y={HORIZON - t.h} width={t.w} height={t.h} />)}
      </g>
      <g fill="#1c1030">
        {scene.mid.map((t, i) => <rect key={i} x={t.x} y={HORIZON - t.h} width={t.w} height={t.h} />)}
      </g>
      <g>
        {scene.midWin.map((w, i) => <rect key={i} x={w.x} y={w.y} width="3" height="5" fill={w.c} opacity="0.55" />)}
      </g>
      <g fill="#0c0814">
        {scene.near.map((t, i) => (
          <g key={i}>
            <rect x={t.x} y={HORIZON - t.h} width={t.w} height={t.h} />
            {t.antenna && <rect x={t.x + t.w / 2 - 1} y={HORIZON - t.h - 26} width="2" height="26" />}
            {t.antenna && <circle cx={t.x + t.w / 2} cy={HORIZON - t.h - 27} r="2.2" fill="#ed1c2e" />}
          </g>
        ))}
      </g>
      <g>
        {scene.nearWin.map((w, i) => <rect key={i} x={w.x} y={w.y} width="4" height="6" fill={w.c} opacity="0.9" />)}
      </g>

      <rect y={HORIZON} width={W} height={H - HORIZON} fill={`url(#${id}-bay)`} />
      {scene.ripples.map((r, i) => (
        <rect key={i} x={r.x} y={r.y} width={r.w} height="1.5" fill={r.c} opacity="0.35" />
      ))}

      {helicopter && (
        <g>
          <g className="searchlight" style={{ transformOrigin: "1060px 96px" }}>
            <polygon points={`1060,96 960,${H} 1120,${H}`} fill={`url(#${id}-beam)`} />
          </g>
          {/* helicopter body, tail, skids and rotor */}
          <g fill="#05060a">
            <ellipse cx="1060" cy="90" rx="17" ry="8" />
            <rect x="1074" y="86" width="34" height="3.5" rx="1.5" />
            <rect x="1104" y="80" width="3" height="11" />
            <rect x="1048" y="100" width="26" height="2" />
            <rect x="1054" y="96" width="2" height="5" />
            <rect x="1066" y="96" width="2" height="5" />
            <rect x="1030" y="79" width="62" height="2" />
          </g>
          <circle cx="1052" cy="88" r="1.6" fill="#ed1c2e" />
        </g>
      )}

      <g fill="none" stroke="#040407" strokeLinecap="round">
        {palms.map((p, i) => (
          <g key={i}>
            <path d={p.trunk} strokeWidth="9" />
            {p.fronds.map((d, j) => <path key={j} d={d} strokeWidth="6" />)}
          </g>
        ))}
      </g>
    </svg>
  );
}
