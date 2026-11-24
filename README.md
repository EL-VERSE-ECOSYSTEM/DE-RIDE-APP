# DE RIDE ecosystem

DE RIDE is a multi-role Nigerian mobility platform monorepo. It contains a Next.js PWA, Go/Fiber API, FastAPI Amebo AI service, and PostgreSQL/PostGIS migrations. Account/session flows, Cloudinary media lifecycle, Paystack wallet funding and ticket checkout, wallet ticket refunds, wallet subscription changes, wallet VTU and Amebo purchases, metered Amebo text chat, wallet-paid bike and parcel jobs with delivery OTP, signed receipts, notification inbox/push, KYC review, trip operations, trip messaging, rank milestones, and customer/admin support workflows are implemented. Remaining provider and operational limits are tracked in `backend/API.md`.

## Local development

Copy `backend/.env.example`, `ai/.env.example`, and `frontend/.env.example` to local `.env` files and supply secrets through your local secret manager. Do not commit credentials. A PostgreSQL instance with PostGIS and pgcrypto is required.

Apply migrations from `backend/` with `go run ../database/scripts/migrate.go`; this uses `DATABASE_URL` and runs ordered, unapplied SQL files transactionally. Seed scripts do not create fake users, park locations, transactions, operational activity, or unapproved rank prizes. Load verified park boundaries, approved reward schedules, and administrator accounts through controlled provisioning. Subscription seeds include only the specified ₦60,000 passenger plan and ₦250 per-bus trip option.

Start services in separate terminals:

```sh
cd backend && go run ./cmd/api
cd ai && uvicorn app.main:app --reload
cd frontend && npm ci && npm run dev
```

The current Go API implements database health, park reads, Argon2id password registration/login, email OTP verification, active-role JWT validation, rider-mode switching, and assigned-operator capacity/departure updates. The cooldown is enforced in Postgres so it holds across API restarts. Amebo chat, trivia, and voice transcription call Groq when configured; its internal key is mandatory. Offline ticket helpers verify P-256 ECDSA payloads and manual ticket checksums, but require a matching backend signer and public key to issue production tickets.

## Financial and safety invariants

The fare helper defines the local Paystack estimate as 1.5% + ₦100, waiving ₦100 below ₦2,500 and capping the local fee at ₦2,000. Ticket refunds currently return 100%, 80%, or 40% of base fare to the DE RIDE wallet; platform and Paystack fees are excluded. Wallet ticket checkout/refund and subscription changes use transactional ledger writes. Paystack wallet funding settles only after provider verification.

Memory cache and websocket state are process-local and must not be used as a financial source of truth. The API exposes authenticated trip chat and an in-process WebSocket hub, with a six-minute message purge worker. Multi-instance deployment needs shared pub/sub or sticky routing for realtime rooms. See `backend/API.md` for provider-backed flows that remain incomplete.

## Deployment

Deploy `frontend/` as the Vercel project root. Set `NEXT_PUBLIC_API_BASE_URL` to the Render API URL plus `/api/v1`, for example `https://deride-api.onrender.com/api/v1`; set `NEXT_PUBLIC_WS_URL` to `wss://deride-api.onrender.com/api/v1/ws` only for authenticated realtime clients. Set `NEXT_PUBLIC_OPENFREEMAP_STYLE` to `https://tiles.openfreemap.org/styles/liberty`. These `NEXT_PUBLIC_*` values are embedded at build time, so configure them before each Vercel build.

The Render blueprints are `backend/render.yaml` and `ai/render.yaml`. The API builds from the repository root; the AI service uses `ai/` as its Docker build context. The API pre-deploy command runs the included migration binary against `DATABASE_URL`; create a Neon database with PostGIS available and supply that URL to Render. Set `ALLOWED_ORIGINS` to the exact Vercel production origin. Set the same high-entropy `INTERNAL_AI_SHARED_SECRET` on both Render services, set API `AI_SERVICE_URL` to the private Render AI service URL, and set `GROQ_API_KEY` on the AI service. Configure `JWT_ACCESS_SECRET` and `JWT_REFRESH_SECRET` as separate secrets of at least 32 characters. SMTP credentials are required for registration OTP delivery.

In-memory cache and WebSocket room membership are per Render process and have no separate cache service charge. Use a single API instance for these realtime rooms; multiple instances do not share rooms. Database migrations are deployed automatically by Render's pre-deploy command.

GitHub Actions run service checks, and `.github/workflows/create-delete-neon-branch.yml` creates expiring PR branches and removes them when PRs close. Set `NEON_PROJECT_ID` as a repository variable and `NEON_API_KEY` as a repository secret.

See `backend/API.md` for implemented APIs, operational dependencies, and remaining workflow gaps; see `database/migrations` for schema rules. Configure verified park data, Cloudinary, Paystack, receipt signing, VAPID, and SMTP before production use. Do not expose unfinished provider-backed flows as completed features.
