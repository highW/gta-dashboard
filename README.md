# The Blacklist Network: roleplay server dashboard

A web dashboard for GTA-style roleplay communities: live players, arrests,
warrants, an audit trail, and role-based access. Work in progress.

```
gta-dashboard/      frontend: React + TypeScript + Vite + Tailwind CSS 3
server/             backend: Go API (MySQL + Redis)
server/lb/          small load balancer in front of two API instances
server/migrations/  SQL change history
```

## Run it locally

You need Docker, Go and Node 22.

**1. Database and cache**

```
docker run -d --name gta-mysql --restart unless-stopped -p 127.0.0.1:3306:3306 -e MYSQL_ROOT_PASSWORD=choose-a-password -e MYSQL_DATABASE=dashboard mysql:8.4
docker run -d --name gta-redis --restart unless-stopped -p 127.0.0.1:6379:6379 redis:7
```

**2. Backend**

```
cd server
copy .env.example .env      # then fill it in (see below)
go run .
```

If Windows refuses port 8080 ("forbidden by its access permissions"), pick
another one: `$env:PORT="18080"` before `go run .`.

Edit `server/.env`:

- `MYSQL_DSN`: for example `root:choose-a-password@tcp(localhost:3306)/dashboard`
- at least one way to sign in: an API key such as `ADMIN_KEY` (use 24+ random
  characters), or Discord login (`DISCORD_*` and `BOOTSTRAP_ADMIN_DISCORD_ID`).
  A key you leave empty switches that role off.

There are no built-in default keys or passwords. Never commit `.env`.

**3. Frontend**

```
cd gta-dashboard
copy .env.example .env      # VITE_API_URL=http://localhost:8080/api  (match your backend port)
npm install
npm run dev
```

Open http://localhost:5173 and sign in with your API key. You can skip the load
balancer locally and point `VITE_API_URL` straight at one backend.

## Checks before you push

```
cd gta-dashboard && npx tsc --noEmit -p tsconfig.app.json && npm run build
cd server && go vet . && go test -count=1 .
```

The Go tests need a MySQL database whose name ends in `_test` (they drop every
table in it) and Redis running:

```
$env:TEST_MYSQL_DSN='root:your-password@tcp(localhost:3306)/dashboard_test'
```

Without that variable the database tests are skipped. CI runs all of this on
every push.

## For designers: where the look lives

- **Colors and surfaces:** CSS variables at the top of
  `gta-dashboard/src/index.css` (`--bg-page`, `--bg-surface`, `--border-color`,
  `--text-primary`, `--accent`, ...). The `.light` block overrides them for
  light mode. Change a variable and the whole app follows.
- **Layout:** `src/layout/AppShell.tsx` (sidebar and top bar).
- **Pages:** `src/pages/` (Command Center, Police, Members, Landing) and
  `src/components/Login.tsx`.
- **Sidebar and Command Center cards:** one list in `src/config/nav.ts`.
- **Icons:** `lucide-react` outline icons.
- **Tailwind is version 3** (config in `tailwind.config.js`). Don't upgrade it
  to 4 without asking; the setup is different.

Please leave alone: `src/context/AuthContext.tsx`, `src/services/*`, and the
`can("...")` permission checks. They mirror rules the server enforces.

**Branding:** don't add Rockstar Games / Take-Two logos, key art or Grand Theft
Auto marks to this repository. Use original artwork only.

## Roles

| Role | Can do |
|---|---|
| viewer | read players, arrests, warrants, stats |
| police | viewer + create and update arrests |
| judge | viewer + issue and clear warrants |
| admin | everything, including members and the audit log |

Roles are enforced on the server for every request; hiding a button in the UI
is only a convenience. People who sign in with Discord get no role until an
admin grants one on the Members page.
