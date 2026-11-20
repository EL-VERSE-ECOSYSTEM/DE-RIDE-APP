# API contract baseline

All endpoints are rooted at `/api/v1`. Authenticated routes require a signed access token and an active role claim. Financial mutations must use an idempotency key and commit a database ledger entry in the same transaction as the business status change.

| Area | Contract |
|---|---|
| Health | `GET /healthz` |
| Auth | `POST /auth/register`, `/auth/login`, `/auth/verify-otp`, `/auth/refresh` |
| Profiles | `GET/PATCH /users/me`, `POST /users/switch-rider-mode` |
| Parks/trips | `GET /parks`, `GET /parks/:id/queue`, `POST /trips`, `PATCH /trips/:id/capacity` |
| Tickets | `POST /trips/:id/checkout`, `GET /tickets/:id`, `POST /tickets/:id/refund` |
| Service orders | `/rides`, `/parcels`, `/data/orders` |
| Realtime | Authenticated websocket `/ws`; rooms `park:{id}`, `trip:{id}`, `admin:sos` |

Error envelope: `{"error":"ERR_CODE","message":"...","retry_after_seconds":120}`. Capacity edits return HTTP 429 `ERR_CAPACITY_COOLDOWN_ACTIVE`. Non-passenger purchase of the ₦60,000 one-time passenger plan returns HTTP 403 `ERR_ROLE_PLAN_RESTRICTED`. Rider-mode switching requires both approved registrations and no in-progress ride or delivery. GPS events are accepted only with explicit GPS opt-in and source precedence HARDWARE > DRIVER_APP > DISPATCHER_APP > MANUAL.

This contract describes intended service boundaries. Routes that depend on real authentication, provider credentials, and database transactions must not be represented as successful until implemented and configured.
