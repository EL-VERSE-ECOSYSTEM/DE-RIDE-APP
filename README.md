# DE RIDE ecosystem

DE RIDE is a multi-role Nigerian mobility platform monorepo. It contains a Next.js PWA, Go/Fiber API, FastAPI Amebo AI service, and PostgreSQL/PostGIS migrations. Account/session flows, Cloudinary media lifecycle, Paystack wallet funding and ticket checkout, wallet ticket refunds, wallet subscription changes, wallet VTU and Amebo purchases, metered Amebo text chat, wallet-paid bike and parcel jobs with delivery OTP, signed receipts, notification inbox/push, KYC review, trip operations, trip messaging, rank milestones, and customer/admin support workflows are implemented. Remaining provider and operational limits are tracked in `backend/API.md`.

## Local development

Copy `backend/.env.example`, `ai/.env.example`, and `frontend/.env.example` to local `.env` files and supply secrets through your local secret manager. Do not commit credentials. A PostgreSQL instance with PostGIS and pgcrypto is required.

Apply migrations from `backend/` with `go run ../database/scripts/migrate.go`; this uses `DATABASE_URL` and runs ordered, unapplied SQL files transactionally. Seed scripts do not create fake users, park locations, transactions, operational activity, or unapproved rank prizes. Load verified park boundaries, approved reward schedules, and administrator accounts through controlled provisioning. Subscription seeds include only the specified ₦60,000 passenger plan and ₦250 per-bus trip option.

Generate and check a local QR signing key pair (the private file is ignored by Git):

```sh
openssl genrsa -out private_key.pem 2048
openssl rsa -in private_key.pem -pubout -out public_key.pem
openssl rsa -in private_key.pem -noout -check
```

Set the key contents as `QR_PRIVATE_KEY_PEM` and `QR_PUBLIC_KEY_PEM` in the backend environment, with a stable `QR_KEY_ID`. Set the matching public key and key ID in the frontend build environment as `NEXT_PUBLIC_QR_PUBLIC_KEY_PEM` and `NEXT_PUBLIC_QR_KEY_ID`. The generated local public key and ID are already set in the ignored `frontend/.env.local`. For local API startup from `backend/`, run `QR_PRIVATE_KEY_PEM="$(cat ../private_key.pem)" QR_PUBLIC_KEY_PEM="$(cat ../public_key.pem)" QR_KEY_ID=deride-rsa2048-20261125 go run ./cmd/api`. Never set the private key in a frontend variable.

Start services in separate terminals:

```sh
cd backend && go run ./cmd/api
cd ai && uvicorn app.main:app --reload
cd frontend && npm ci && npm run dev
```

The current Go API implements database health, park reads, Argon2id password registration/login, email OTP verification, active-role JWT validation, rider-mode switching, and assigned-operator capacity/departure updates. The cooldown is enforced in Postgres so it holds across API restarts. Amebo chat, trivia, and voice transcription call Groq when configured; its internal key is mandatory. Offline ticket helpers verify RSA-2048 / SHA-256 signed payloads and manual ticket checksums. The booking screen renders signed QR tickets with the DE RIDE mark, and uses idempotent checkout plus a server-side seat recheck.

## Financial and safety invariants

The fare helper defines the local Paystack estimate as 1.5% + ₦100, waiving ₦100 below ₦2,500 and capping the local fee at ₦2,000. Ticket refunds currently return 100%, 80%, or 40% of base fare to the DE RIDE wallet; platform and Paystack fees are excluded. Wallet ticket checkout/refund and subscription changes use transactional ledger writes. Paystack wallet funding settles only after provider verification.

Memory cache and websocket state are process-local and must not be used as a financial source of truth. The API exposes authenticated trip chat and an in-process WebSocket hub, with a six-minute message purge worker. `docker-compose.lb.yml` and `deployment/nginx-api.conf` provide a three-instance NGINX deployment with client IP affinity and WebSocket proxying. Use `docker compose --env-file backend/.env -f docker-compose.lb.yml up --build` and point the frontend API URL at the load balancer. Affinity preserves a client's room routing while an instance stays healthy; the Render deployment remains single-instance, and shared pub/sub is still required for realtime continuity after instance changes. See `backend/API.md` for provider-backed flows that remain incomplete.

## Deployment

For a self-hosted deployment, `docker-compose.lb.yml` starts three API containers behind the Nginx proxy in `deployment/nginx-api.conf`. It forwards WebSocket upgrades and uses IP affinity for realtime rooms. Supply `backend/.env` before starting it. Keep the Render deployment at one API instance until realtime rooms and process-local caches move to shared infrastructure; IP affinity is not a replacement for shared state after a worker restart or failover.

Deploy `frontend/` as the Vercel project root. Set `NEXT_PUBLIC_API_BASE_URL=https://de-ride-app.onrender.com/api/v1` and `NEXT_PUBLIC_WS_URL=wss://de-ride-app.onrender.com/api/v1/ws` for authenticated realtime clients. Set `NEXT_PUBLIC_OPENFREEMAP_STYLE` to `https://tiles.openfreemap.org/styles/dark` for the logo-matched dark map. These `NEXT_PUBLIC_*` values are embedded at build time, so configure them before each Vercel build. For self-hosted Photon/Nominatim, set the server-only `GEOCODER_DOMAIN` variable as documented in `deployment/geocoding/README.md`; leave it blank in local development to use public fallback APIs. Never place `NOMINATIM_PASSWORD` in frontend environment variables.

The Render blueprints are `backend/render.yaml` and `ai/render.yaml`. The API Docker build context must be the repository root because `backend/Dockerfile` copies both `backend/` and `database/`; `backend/render.yaml` sets `dockerContext: .` and points to `backend/Dockerfile`. In the Render dashboard, clear any service Root Directory override so it does not hide those folders. The AI service uses `ai/` as its Docker build context. The API pre-deploy command runs the included migration binary against `DATABASE_URL`; create a Neon database with PostGIS available and supply that URL to Render. Set `ALLOWED_ORIGINS` to the exact Vercel production origin. Set the same high-entropy `INTERNAL_AI_SHARED_SECRET` on both Render services, set API `AI_SERVICE_URL=https://de-ride-app-ai.onrender.com`, and set `GROQ_API_KEY` on the AI service. Configure `JWT_ACCESS_SECRET` and `JWT_REFRESH_SECRET` as separate secrets of at least 32 characters. SMTP credentials are required for registration OTP delivery. Set backend `QR_PRIVATE_KEY_PEM`, `QR_PUBLIC_KEY_PEM`, and `QR_KEY_ID` in Render; set frontend `NEXT_PUBLIC_APP_URL`, `NEXT_PUBLIC_QR_PUBLIC_KEY_PEM`, and `NEXT_PUBLIC_QR_KEY_ID` in Vercel before building so generated sitemaps and offline verification use the production host/key. The locally generated values are in the Git-ignored `deployment/qr-render.env`; copy them into the matching service variables and keep the private key only on the API service.

Render provides the public service entry point and currently runs one API instance. The included NGINX Compose setup is for self-managed deployments; for high availability across API restarts, add shared pub/sub and distributed cache/cooldown storage. Database migrations are deployed automatically by Render's pre-deploy command.

GitHub Actions run service checks, and `.github/workflows/create-delete-neon-branch.yml` creates expiring PR branches and removes them when PRs close. Set `NEON_PROJECT_ID` as a repository variable and `NEON_API_KEY` as a repository secret.

See `backend/API.md` for implemented APIs, operational dependencies, and remaining workflow gaps; see `database/migrations` for schema rules. Configure verified park data, Cloudinary, Paystack, receipt signing, VAPID, and SMTP before production use. Do not expose unfinished provider-backed flows as completed features.
