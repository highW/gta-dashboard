# The Blacklist Network: roleplay server dashboard

A web dashboard for GTA-style roleplay communities: live players, arrests,
warrants, an audit trail, and role-based access. Work in progress.

```
gta-dashboard/      frontend: React + TypeScript + Vite + Tailwind CSS 3
server/             backend: Go API (MySQL + Redis)
server/lb/          small load balancer in front of two API instances
server/migrations/  SQL change history
docker/             files used by the containers (MySQL setup, smoke test)
compose.yaml        the whole stack in containers
compose.dev.yaml    only MySQL and Redis, for coding
```

## Run everything with Docker

You need only Docker (Docker Desktop on Windows and macOS).

```
copy .env.example .env      # then fill it in: two MySQL passwords and at least ADMIN_KEY
docker compose up -d --build
```

Open http://localhost:8000 and sign in with your `ADMIN_KEY`. The first start
takes a minute or two: images are built and MySQL creates its tables and the
app's limited database user. A fresh database gets a few demo players.

```
browser ──► web (nginx) ──► lb ──┬──► api-1 ──┬──► mysql
                                 └──► api-2 ──┴──► redis
```

Only `web` is reachable from outside. The others talk over a private Docker
network and find each other by service name (`mysql`, `redis`, `lb`...).
MySQL and Redis keep their data in Docker volumes, so `docker compose down`
and `up` keep everything; `docker compose down -v` deletes it.

After changing code: `docker compose up -d --build` again. Useful while it
runs: `docker compose ps`, `docker compose logs -f api-1`.

To sign in with Discord, fill in `DISCORD_*` in `.env` and register
`<PUBLIC_URL>/api/auth/discord/callback` as a redirect in the Discord
developer portal.

## Run it locally

You need Docker, Go and Node 22.

**1. Database and cache**

```
copy .env.example .env      # in the repository root: the two MySQL passwords
docker compose -f compose.dev.yaml up -d
```

This starts only MySQL and Redis, reachable on `localhost` from this machine.

**2. Backend**

```
cd server
copy .env.example .env      # then fill it in (see below)
go run .
```

If Windows refuses port 8080 ("forbidden by its access permissions"), pick
another one: `$env:PORT="18080"` before `go run .`.

Edit `server/.env`:

- `MYSQL_DSN`: `dashboard_app:<MYSQL_APP_PASSWORD from the root .env>@tcp(localhost:3306)/dashboard`
- at least one way to sign in: an API key such as `ADMIN_KEY` (use 24+ random
  characters), or Discord login (`DISCORD_*` and `BOOTSTRAP_ADMIN_DISCORD_ID`).
  A key you leave empty switches that role off.
- optional: `REDIS_URL` when Redis runs on another machine, and
  `TRUSTED_PROXIES` when the load balancer does (both are explained in
  `.env.example`).

There are no built-in default keys or passwords. Never commit `.env`.

Already have a database from an earlier version? A new database needs nothing
extra, but an existing one has to be brought up to date with the newer files
in `server/migrations/`, in order. Each file's header says what it does and
how to run it safely.

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
| police | viewer + log arrests, request warrants |
| supervisor | police + void a record made in error, read the audit log |
| prosecutor | viewer + decide arrests (convicted / released), request warrants |
| judge | viewer + approve or deny warrant requests, issue and clear warrants |
| admin | everything, including members and deleting players |

A warrant moves through `requested → active → cleared`, or `requested →
denied`. A judge's own warrant starts as active. Arrests start as `pending`
until a prosecutor decides them; a voided arrest stays on record and never
changes again. Judges and prosecutors work from the **Judicial** page.

Roles are enforced on the server for every request; hiding a button in the UI
is only a convenience. People who sign in with Discord get no role until an
admin grants one on the Members page.
