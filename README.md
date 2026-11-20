# DE RIDE ecosystem

DE RIDE is a multi-role Nigerian mobility platform. This repository contains the Next.js PWA, Go API, FastAPI Amebo service, and PostgreSQL/PostGIS schema.

## Local development

Copy the relevant `.env.example` to the service's local environment file and provide real provider credentials. Never commit secrets. PostgreSQL must have PostGIS enabled. Start the API with `cd backend && go run ./cmd/api`; start Amebo with `cd ai && uvicorn app.main:app --reload`; start the web app with `cd frontend && npm install && npm run dev`.

The API requires `DATABASE_URL`, JWT secrets, and `INTERNAL_AI_SHARED_SECRET`. Payment, mail, maps, Cloudinary, Groq and VTU operations require their corresponding credentials. Missing integrations fail closed rather than returning simulated success. Apply SQL migrations in numerical order using `database/scripts/migrate.go` or your PostgreSQL migration runner. Seed files contain reference configuration only; they do not fabricate users, transactions, trips, or operational activity.

## Financial invariants

Ticket charges are base fare + ₦20 platform fee (unless explicitly waived) + Paystack fee. The local Paystack estimate is 1.5% + ₦100, with the ₦100 waived below ₦2,500 and the local fee capped at ₦2,000. Provider verification is authoritative before a payment is settled. Paystack and platform fees are excluded from fare refunds; cancellation, late arrival and no-show refund 100%, 80% and 40% of refundable base fare respectively. Refunds are idempotent ledger entries.

Daily Amebo allowances use Africa/Lagos day boundaries and expire; purchased bundles remain available until used. Cache and websocket state are process-local and are not used as a financial source of truth. Multi-instance deployments need sticky websocket routing or a shared pub/sub adapter before horizontal scaling.

See `backend/API.md` for route contracts and `database/migrations` for schema rules. This repository provides deployable service foundations; production launch still requires real credentials, operational park/route data, provider verification, security review, and deployment configuration.
