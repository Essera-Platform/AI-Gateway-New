# Changes made by Essera

This repository is Essera's modified version of [new-api](https://github.com/QuantumNous/new-api), licensed under the GNU AGPL v3 with the additional terms in `NOTICE`. This file lists what Essera changed, as AGPLv3 §5(a) and §7(c) ask. Every upstream file Essera modified carries a `Modified by Essera` comment at the change.

Essera adds identifiers and never removes upstream ones. The rules for Issues and Pull requests in `AGENTS.md` → Project Governance are upstream's contribution policy and apply only when contributing to new-api.

## 2026-11 — Build, deployment and observability

- **CI:** GitHub Actions workflows build the backend and frontend images, push them to Amazon ECR and deploy them to the `essera-dev` ECS cluster. The upstream workflows are kept in `.github/workflows-disabled/`.
- **Dockerfiles:** `Dockerfile.backend` and `Dockerfile.frontend`. The frontend is an nginx image that proxies the API and relay paths to the backend.
- **Observability:** OpenTelemetry export from the backend (`pkg/telemetry`) and Grafana Faro in the web frontend.

## 2026-11 — Integration with the Essera Operations Console

When `ESSERA_MANAGED=true`, the Gateway is administered by the Essera Operations Console. The Console holds providers, customers, entitlements, limits, prices and settings, and publishes them as immutable configuration snapshots. The Gateway applies them and reports back. Without the variable, behaviour is upstream's.

### New code (`essera/`)

- **`essera/configsync`:** reads the configuration queue; fetches each snapshot from S3 and checks its SHA-256; keeps the last good version in the database; projects the Console's providers into channels (one channel per provider, tagged `essera/<key>`, in group `essera`). Only events naming this environment's snapshot bucket and prefix are applied, and none more than 1000 versions ahead of the active one; a version that fails on the way (not an unreadable document) is retried by the queue.
- **`essera/secrets`:** resolves provider keys from AWS Secrets Manager when a channel is selected. Channels store only a `secretsmanager:` reference, never the key.
- **`essera/auth`:** the caller of a customer request, and the customer pool's signing keys, which verify people's ID tokens at sign-in. The keys are fetched at most once a minute, so tokens naming unknown keys cannot make the Gateway call Cognito on every request. In managed mode, relay paths accept only the API keys people create (`essera/keys`); a new-api key is refused, and so is a Cognito access token. The Cognito client-credentials tokens the Console's app clients ("service credentials") once carried are no longer accepted: the Console retired those clients, and a snapshot that still lists one (`kind` `cognito_client` or none) authenticates nothing.
- **`essera/policy` and `essera/limits`:** enforce the snapshot before routing:
  - maintenance, including a scheduled window once it has begun;
  - partner, customer and end-user status;
  - approved and blocked countries;
  - models and providers;
  - monthly request, token and spend limits, checked against the totals the Console publishes from its ledger (`usage.totals` on the configuration queue, kept in the database). Spend is the Console's figure only; the Gateway prices nothing. Requests and tokens are the Console's total plus what the Gateway counted from the totals' `as_of` on: a request is reserved when the relay takes it, so concurrent requests cannot all take the last one, and given back if it reaches no provider; tokens are added when it ends, whatever its outcome. Until totals arrive, limits use zero spend and the Gateway's own counts, and this is logged.
- **Snapshot entitlements:** each account level publishes countries, providers, models, monthly request, token and spend limits, currency and billing cycle; a child narrows its parent. A field the Gateway does not read is ignored, so a snapshot from an older Console that still carries the account's pricing plan applies as before.
- **`essera/providerlimits`:** applies each provider's settings from the snapshot to every attempt:
  - the rate limit in requests per minute, counted across nodes in the database, with 429 `essera_provider_rate_limited` past it;
  - the provider's retries as the request's retry budget;
  - its timeout as the time the provider has to start answering.
- **`essera/usage`:** one `usage.recorded` event per request a customer had served, sent to the usage queue through a database outbox (events SQS keeps refusing are set aside after 20 attempts, so they never hold back the rest). Images are counted as settled, audio in minutes of sound. The event measures and names no rate version; the Console rates it.
- **`essera/stats` and `essera/bridge`:** request statistics, and the internal API the Console's backend reads (`/internal/essera/v1`: state and active configuration version, a provider's connection test, a provider's telemetry, and the traffic of the last hour, day or week: requests, latency, HTTP statuses, denials and each provider's availability, latency, retries and timeouts). It is reached inside the cluster only.
- **`essera/servicetrust`:** authenticates the Console's backend on the internal API by its AWS role. The backend sends an `sts:GetCallerIdentity` request signed by its task role; the Gateway forwards it to STS (its global or a regional endpoint, nothing else) and serves only the ARNs in `ESSERA_TRUSTED_PRINCIPALS`. A header named twice is refused, and a verified identity is not reused past its signature's lifetime. No secret is shared.
- **`essera/bootstrap`:** sets up the service user and token that customer requests run as. Its group's ratio is zero, so new-api neither reserves nor charges quota; the Console bills. It turns off new-api's consume log, which would duplicate the Console's ledger, and replaces the default root password. It names the gateway for Essera (`ESSERA_SYSTEM_NAME`, `ESSERA_LOGO`).
- **`essera/managed`:** in managed mode, refuses local edits of what the Console owns (channels, users, tokens, prices, models) and of plugins, deployments and sign-in providers, closing an account, and new-api's OAuth sign-in, and closes the features a managed gateway does not offer (below). A person may create and delete their own keys. A person's API key may only POST to `/v1/chat/completions`, `/v1/responses`, `/v1/messages` and `/v1/embeddings` and GET `/v1/models`; a GET on `/v1/responses` would open a WebSocket whose calls are not accounted for.
- **`essera/configsync` people:** each person the snapshot publishes (`accounts.people`) becomes one new-api user, linked by `oidc_id` = Cognito sub and the `essera_people` table, with new-api's common-user role and the person's Gateway role, `admin` or `user`, in `essera_people`; an admin is never a new-api administrator. Nothing else of the person is kept. A person removed or suspended in the Console is disabled at the next version, and their sessions and Cognito sign-ins end at once.
- **`essera/identity`:** the Gateway's own sign-in for people, through the Cognito user-pool API of the customer pool (no hosted page, no redirect): `InitiateAuth` `USER_AUTH`, then `RespondToAuthChallenge` with the emailed code (`ESSERA_SIGN_IN=email_otp`, the default) or the password (`password`), and a new password on a first password sign-in. The Cognito session between two steps stays on the server, sealed, behind an HttpOnly cookie, and each step is spent when used. Every wrong email, password or code, unknown or disabled user and inactive person gets the same answer; an unknown email is asked for a code too. Steps are throttled per email and per IP across nodes. The ID token is verified against the pool's keys, issuer, app client and use, and its sub must be an active person. The Cognito refresh token is kept sealed on the server with the Gateway session; each session refresh renews it (`GetTokensFromRefreshToken`, or `REFRESH_TOKEN_AUTH` without rotation) and ends the person's sessions when Cognito refuses it or the person is no longer active; signing out revokes it (`RevokeToken`). Operators (root) keep new-api's password sign-in; nobody else may use it.
- **`essera/keys`:** a person creates their own `sk-` keys (90 days unless shorter is chosen) and deletes them. The Gateway keeps only the key's id; the key is shown once. `credential.created` and `credential.revoked` go to the Console on the usage queue through the outbox, with a `type`. On relay a key is accepted only while the active snapshot publishes it as an `api_key` credential with its id and SHA-256, unexpired, of an active person of the same customer, and it is not deleted on the Gateway; it then runs as that customer and, for role `user`, that end user, through the Console's policy, limits and usage, with the key id as `credential_ref`.
- **`essera/consoleapi`:** reads the Console's internal API (`/internal/v1` at `CONSOLE_INTERNAL_URL`) for a signed-in person: their account, usage, usage totals, audit, prices and the tenant's people, and asks the Console to add, suspend, reactivate or remove an end user. Each call proves the Gateway's task role with a signed `sts:GetCallerIdentity` for the audience `essera-console-<env>` (`essera/servicetrust.Identity`, the Console's own mechanism the other way round) and names the person in `X-Essera-Person`. When the Console does not answer, the pages say the data is temporarily unavailable; relay never depends on it.
- **Pages of the Console's people:** usage logs, their totals, the dashboard series, audit and the account summary (spend, limits and what is left) come from the Console in new-api's response formats; the Gateway calculates no money. The Model Square shows a signed-in person only the models relay lets them call (`policy.Available`), with the prices the Console gives for them, and a person who is not active nothing; a guest sees the snapshot's active models without prices. An admin sees their customer's people on the Users page and adds, suspends, reactivates and removes users there; the Gateway checks that the caller is an active admin and the target a user (role `user`) of the same customer, never an admin or themselves, before the Console does the same. Operators keep new-api's own pages.
- **Playground:** runs as the signed-in person, through the same policy, limits and usage as their API calls; anyone else is refused.
- **Model lists:** a person's model list (`/api/user/models`, used by the playground) holds only what relay lets them call (`policy.Available`, the rules `policy.Evaluate` applies): active models of active providers within the customer's entitlements and, for a user, their own limits; nothing while they are not active.
- **Service status:** the Overview shows an admin a "Service status" panel in place of new-api's Uptime Kuma panel, from the Console's `GET /internal/v1/status` through `GET /api/essera/status` (admins only): per provider the state (operational, degraded, down or maintenance) with a status dot, the region, last hour's availability and median latency, and when it was checked, and a banner for maintenance Essera announced. A user does not see it, and while the Console does not answer the panel says the status is temporarily unavailable. `/api/uptime/status` stays closed.
- **Groups:** everyone is in group `essera`; group choices are hidden, and the Model Square, its model details and the usage logs show no group.
- **`web/src/features/essera`:** in managed mode, the Essera sign-in (email, then the emailed code with resend and expiry, or the password and a first new password), without a link to the operators' sign-in, which only `/sign-in?operator=1` opens; creating a key with only a name and an expiry and showing it once, with a ready-to-run example request; and a badge in the admin header says the Console administers the gateway, and its tooltip says what is changed there.

### What a managed gateway does not offer

One deny-list in `essera/managed` closes what the Gateway does not use when the Console administers it. Upstream code stays in place, so upstream merges stay easy; the web UI hides the matching entry points.

- **Closed to everyone** (`managed.Guard`, 404 `essera_not_available`):
  - Midjourney, Suno, video and tasks, and their logs: `/mj/*`, `/:mode/mj/*`, `/suno/*`, `/v1/video*`, `/v1/videos*`, `/v1/tasks*`, `/api/mj/*`, `/api/task*`;
  - new-api's OAuth sign-in and binding (GitHub, Discord, LinuxDO, Telegram, WeChat, OIDC, custom providers, email binding), password reset and emailed codes: `/api/oauth/*`, `/api/verification`, `/api/reset_password`, `/api/user/reset`;
  - the wallet, payments and their callbacks, subscriptions, redemption codes, referrals and check-in: `/api/user/topup*`, `/api/user/amount`, `/api/user/pay`, `/api/user/stripe/*`, `/api/user/creem/*`, `/api/user/waffo/*`, `/api/user/waffo-pancake/*`, `/api/user/aff`, `/api/user/aff_transfer`, `/api/user/epay/*`, `/api/user/checkin`, `/api/stripe|creem|waffo|waffo-pancake/webhook*`, `/api/subscription/*`, `/api/redemption/*`;
  - content the Console does not publish, and figures worked out from every customer's traffic or prices: `/api/about`, `/api/notice`, `/api/uptime/status`, `/api/rankings`, `/api/ratio_config`;
  - a new-api key's quota and logs: `/api/usage/token`, `/api/log/token`.
- **Closed to people, kept for operators (root)** (`managed.PersonRouteRefused`, called by dashboard authentication once the role is known; 404): new-api's access tokens `/api/user/access_tokens*`, Passkeys `/api/user/passkey*` (the operators' Passkey sign-in stays), two-factor authentication `/api/user/2fa/*`, step-up verification `/api/verify*`, sign-in bindings `/api/user/oauth/bindings*`, and the notification, unpriced-model and IP-recording settings `PUT /api/user/setting`. A person's unpriced-model setting stays on, as the people projection sets it: the Console prices every request.
- **Already refused before this change, unchanged:** a person's password or username (`PUT /api/user/self`, 409), closing an account (`DELETE /api/user/self`, 409), self-registration (`/api/user/register`, 409), reading a key back for chat deep links (`POST /api/token/:id/key`, `/api/token/batch/keys`, 409), and new-api's balance for keys (`/dashboard/billing/*`, `/v1/dashboard/billing/*`, 404 to keys).
- **Relay:** legacy completions, images, audio, rerank, moderations, realtime, Gemini's format (`/v1/models/*` POST, `/v1beta/*`) and response compaction are refused to API keys (404): Bedrock serves none of them in the Console's region, or they produce no usage event. Plugin-defined routes need a key too, so the same rule closes them.
- **`/api/status`** (`managed.TrimStatus`): no chat links (`chats`), no documentation link (`docs_link`), and announcements, uptime, check-in, registration and new-api's OAuth sign-in switched off.
- **Web UI** in managed mode: no Chat clients in the sidebar and no `/chat` or `/chat2link` pages; no Docs, About or Rankings link, no docs.newapi.pro link on the home page or in the footer, and the About, Rankings, sign-up, password reset and OAuth pages go to the home page; no notifications bell; the playground has no Search button; the sidebar settings offer no switch for chat clients, drawing and task logs or the wallet. A person's profile has no notification or unpriced-model settings, and their Security & Access page has only their login sessions. The usage logs ask nothing about subscriptions, and operators' user actions offer none.

### Upstream files changed

| File | Change |
| --- | --- |
| `main.go` | Starts the integration before the node sync; installs the managed-mode guard |
| `middleware/auth.go` | `TokenAuth` hands a customer's API key to the integration before the key is split, and narrows the service token's models to the customer's |
| `middleware/essera.go` (new) | The customer authentication and policy middleware |
| `middleware/utils.go` | Counts a customer's request that new-api refuses after the Essera policy (a model outside the entitlements, no channel), which never reaches relay |
| `controller/relay.go` | Reports every finished relay request to the integration; reserves a customer's request against its quotas; admits each attempt against the provider's rate limit and takes the provider's retries as the retry budget |
| `relay/channel/api_request.go` | Bounds the wait for a provider's answer by the provider's timeout |
| `controller/essera.go` (new) | Runs the Console's connection tests through new-api's channel test |
| `router/main.go` | Registers the internal API |
| `controller/misc.go` | `/api/status` says whether the gateway is managed (`essera_managed`) |
| `web/src/components/layout/components/app-header.tsx` | Shows the managed badge |
| `model/channel.go` | `GetNextEnabledKey` resolves a key that is a Secrets Manager reference |
| `model/essera_hooks.go` (new) | The hook that resolution goes through |
| `dto/channel_constraints.go`, `model/channel_constraint.go` | A channel filter that limits a model to the providers a customer may use |
| `web/src/components/layout/components/footer.tsx`, `web/src/i18n/locales/*.json` | The footer shows the AGPLv3 §7(b) notice from `NOTICE` verbatim; the locales also carry the managed badge's texts |
| `controller/essera.go` | The Essera sign-in handlers, a person's key creation, and refusing a person's username or password change |
| `controller/auth_session.go` | A session refresh renews the Cognito sign-in behind it; signing out revokes it |
| `controller/user.go` | Only operators sign in with new-api's password or Passkey; a person cannot change their username or set a password; a person's model list is what relay lets them call |
| `controller/token.go` | Creating and deleting a key is a person's Console credential |
| `controller/misc.go` | `/api/status` says how people sign in (`essera_sign_in`) |
| `controller/misc.go` | On a managed gateway, `/api/status` gives the public address (`ESSERA_PUBLIC_URL`) as `server_address` |
| `web/src/features/dashboard/components/overview/overview-dashboard.tsx` | In managed mode, the first request names the public address and `$ESSERA_API_KEY`, and Copy does not read the key back |
| `router/api-router.go`, `router/access_token_scope_test.go` | The anonymous `/api/essera/sign-in` routes |
| `router/relay-router.go` | The playground runs through the Essera policy |
| `service/group.go` | A managed gateway offers only the `essera` group |
| `web/src/features/auth/sign-in/index.tsx` | The Essera sign-in in managed mode |
| `web/src/features/keys/components/*` | In managed mode, the key is shown once; edit, enable, reveal, quota and group are hidden |
| `web/src/components/model-group-selector.tsx`, `web/src/features/playground/components/input/playground-input-controls.tsx` | The playground hides the group choice in managed mode |
| `controller/essera_pages.go` (new) | The pages of the Console's people, the tenant's people, the service status and the Model Square |
| `controller/log.go`, `controller/usedata.go`, `controller/access_token.go`, `controller/user.go`, `controller/pricing.go` | A person's usage logs, dashboard, audit and account summary, and the managed Model Square, come from the Console |
| `router/api-router.go`, `middleware/access_token_routes.go` | The admin's `/api/essera/people` routes, browser sessions only |
| `router/api-router.go`, `middleware/access_token_routes.go` | The admin's `/api/essera/status` route, browser sessions only |
| `essera/awsx/awsx.go` | The task role's credentials, to sign the Gateway's identity |
| `web/src/features/essera/*`, `web/src/features/pricing/*`, `web/src/features/usage-logs/*`, `web/src/features/dashboard/*`, `web/src/features/profile/*`, `web/src/hooks/use-sidebar-*.ts`, `web/src/routes/_authenticated/route.tsx`, `web/src/routes/_authenticated/users/index.tsx`, `web/src/components/layout/components/mobile-drawer.tsx`, `web/src/stores/auth-store.ts` | In managed mode: the Console's figures, the tenant's people and the service status for its admin, the Model Square's models and prices, and the menus and pages the Console owns closed |
| `web/src/features/pricing/*` | In managed mode, no groups; a model nobody has called says "No requests yet" instead of empty metrics |
| `web/src/features/usage-logs/audit/lib/audit-details.ts` | Names each of the Console's audit events by its action ("API key created", "User suspended"), with the raw action for one it does not know |
| `web/src/routes/(auth)/sign-in.tsx`, `web/src/features/auth/sign-in/index.tsx` | The operators' sign-in only at `/sign-in?operator=1` on a managed gateway |
| `web/src/components/layout/components/public-header.tsx` | The compact header is wide enough for the Essera name and every link |
| `essera/managed/managed.go` | The managed-mode deny-lists: routes closed to everyone, routes closed to people, the relay routes a key may call, and the `/api/status` trim |
| `middleware/auth.go` | Dashboard authentication refuses a person the routes closed to people |
| `controller/misc.go` | `/api/status` is trimmed in managed mode |
| `web/src/features/essera/menu.ts`, `web/src/features/essera/use-essera-managed.ts`, `web/src/routes/__root.tsx` | The pages a managed gateway does not serve, public ones included; who is one of the Console's people |
| `web/src/hooks/use-top-nav-links.ts`, `web/src/features/home/components/sections/hero.tsx`, `web/src/components/layout/components/footer.tsx` | No Docs, About or Rankings link and no docs.newapi.pro link in managed mode; Get Started opens the sign-in |
| `web/src/hooks/use-notifications.ts`, `web/src/components/layout/components/app-header.tsx`, `web/src/components/layout/components/public-header.tsx` | No notifications bell, and no notice request, in managed mode |
| `web/src/features/playground/components/input/playground-input-tools.tsx` | No Search button in managed mode |
| `web/src/features/profile/index.tsx`, `web/src/features/profile/components/sidebar-modules-card.tsx`, `web/src/features/security/index.tsx` | A person's profile and Security & Access page in managed mode |
| `web/src/features/auth/sign-in/components/user-auth-form.tsx`, `web/src/features/usage-logs/components/usage-logs-table.tsx`, `web/src/features/users/components/data-table-row-actions.tsx` | No password reset link, subscription lookup or subscription action in managed mode |
| `go.mod`, `go.sum`, `THIRD-PARTY-LICENSES.md` | AWS SDK modules for S3, SQS, Secrets Manager and shared configuration, and go-jose for the customer pool's signing keys (Apache-2.0) |

| `controller/misc.go`, `essera/managed/managed.go`, `essera/snapshot/view.go` | On a managed gateway, `/api/status` gives the landing page the live count of the catalog's active models and of their families (`essera_catalog`); it names no account |
| `controller/essera.go` | On a managed gateway nobody saves an interface language (`PUT /api/user/self` with `language`, 404 `essera_not_available`): the gateway is English only |
| `web/src/styles/*.css`, `web/index.html`, `web/src/context/*`, `web/src/components/theme-*.tsx`, `web/src/components/config-drawer.tsx`, `web/src/components/command-menu.tsx`, `web/src/components/language-switcher.tsx`, `web/src/components/ui/sidebar.tsx`, `web/src/components/layout/components/*`, `web/src/hooks/use-sidebar-data.ts`, `web/src/hooks/use-top-nav-links.ts`, `web/src/i18n/config.ts`, `web/src/lib/status-query.ts`, `web/src/main.tsx`, `web/src/features/auth/*`, `web/src/features/home/index.tsx`, `web/src/features/profile/index.tsx`, `web/src/features/dashboard/components/*` | The Essera design (below) |

### Settings

| Variable | Meaning |
| --- | --- |
| `ESSERA_MANAGED` | `true` turns the integration on |
| `ESSERA_ENVIRONMENT` | `dev`, `uat` or `prod`; names the queues (`essera-{env}-config-updates`, `essera-{env}-usage-events`) |
| `ESSERA_TRUSTED_PRINCIPALS` | IAM ARN patterns allowed to call the internal API, e.g. `arn:aws:sts::<account>:assumed-role/essera-dev-task/*` |
| `ESSERA_PROVIDER_TYPES` | Optional adapter per provider key, `{"acme-llm": 1}`, for keys the defaults do not recognise |
| `ESSERA_FALLBACK_POLICIES` | Optional, `{"fallback/openai-to-azure": ["azure-openai"]}` |
| `ESSERA_OIDC_ISSUER`, `ESSERA_OIDC_CLIENT_ID`, `ESSERA_OIDC_CLIENT_SECRET` | The customer pool (`https://cognito-idp.<region>.amazonaws.com/<pool id>`) and the app client people sign in with |
| `CONSOLE_INTERNAL_URL` | The Console's internal API inside the cluster, e.g. `http://essera-backend:8000` |
| `ESSERA_SYSTEM_NAME`, `ESSERA_LOGO` | The name the web UI shows (default `Essera AI Gateway`) and, optionally, the address of its logo; set at start in managed mode, where the options are locked. The new-api attribution in the footer stays |
| `ESSERA_SIGN_IN` | `email_otp` (default; the pool must send email through SES) or `password` |
| `ESSERA_PUBLIC_URL` | Optional: the address people call the Gateway at, shown in the web UI and its example requests. Without it, the first `SESSION_COOKIE_TRUSTED_URL`; without either, new-api's `ServerAddress` |
| `ESSERA_AWS_ENDPOINT`, `ESSERA_JWKS_URL`, `ESSERA_PROVIDER_BASE_URLS` | Development only: local stand-ins for AWS, Cognito and providers |

### Database

The fork runs on PostgreSQL only. The Essera tables are:

- `essera_config_versions`
- `essera_usage_outbox`
- `essera_console_totals` (the Console's latest usage totals per subject and month)
- `essera_local_use` (requests and tokens per subject and second since the Console's totals; `essera_usage_counters` is dropped)
- `essera_provider_minutes`
- `essera_request_stats`
- `essera_provider_errors`
- `essera_people`, `essera_keys`, `essera_cognito_sessions`, `essera_sign_in_attempts`

They hold working state; every business record stays in the Console.

## 2026-11 — AWS Bedrock models in eu-west-1

- **Text through Converse:** every model except Claude, Amazon Nova included, goes through Converse and ConverseStream. This covers system prompts, images, documents, video, `input_audio` (Voxtral), tools, stop, sampling, max tokens, cache points and reasoning effort (GPT-oss, Nova 2; at high effort Nova 2 refuses temperature and top_p, so they are dropped). Answers are served as chat completions, Anthropic Messages or Responses. Cache reads and writes count as prompt tokens.
- **Nova 2 at high reasoning effort with a token cap:** Bedrock refuses `maxTokens` at that effort. The cap is what the caller pays at most, so the Gateway neither drops it nor lowers the effort: it answers 400 `invalid_request_error` itself, before calling Bedrock, with "Nova 2 at high reasoning effort does not accept max_tokens: remove max_tokens or use medium reasoning effort." This applies to `max_tokens` and `max_completion_tokens` on chat completions, `max_output_tokens` on Responses, and `max_tokens` on Anthropic Messages. Messages requires `max_tokens`, so Nova 2 at high effort is not available through Messages; use medium effort there, or chat completions or Responses without a cap. `xhigh` counts as high.
- **Claude** stays on InvokeModel, except on `/v1/responses`, which uses Converse for every model.
- **Embeddings:** Titan Text Embeddings v2, Titan Multimodal Embeddings, and Cohere Embed v3 and v4, through InvokeModel with each model's native body.
- **Errors:** Bedrock exceptions map to 429, 400, 403, 404, 503, 504 or 401. Rerank is refused, because it is not offered in eu-west-1. The Gemini request format is not converted for Bedrock.
- **Files:** `relay/channel/aws/adaptor.go`, `relay-aws.go`, `dto.go` and `mantle.go` were changed. `essera_runtime.go`, `essera_converse.go`, `essera_embedding.go` and `essera_bedrock_test.go` are new.
- **Inference profiles:** in eu-west-1 many models are served only through the `eu.` inference profile; Bedrock refuses their plain id with a ValidationException saying on-demand throughput isn't supported. The Gateway then calls the same request once more through the region's geographic profile (`eu.`, `us.` or `apac.`, never `global.`, so the data stays in the geography) and remembers this per channel, region and model, so later calls go straight to the profile. An id that already names a profile (`eu.`, `us.`, `apac.`, `global.`, an ARN) is never changed, any other error is not retried, and a request is never repeated once the client got any bytes. Nova 2 Lite, Claude Opus 5, Claude Sonnet 5 and Pixtral Large are added to upstream's cross-region table for `eu`, so their first call needs no retry. Usage and logs keep the catalog model; the profile is only how Bedrock is called. This covers Converse, ConverseStream, embeddings and Claude on InvokeModel, and the Console's connection test, which runs through the same adaptor. Files: `essera_runtime.go`, `adaptor.go`, `relay-aws.go` and `constants.go`.
- **Image and document limits:** a chat completions or Responses request with more than 20 images or 5 documents, an inline image over 3.75 MB or an inline document over 4.5 MB is refused with 400 `invalid_request_error` naming the limit, before Bedrock is called, on Converse and on Claude's InvokeModel alike. Linked media are counted; their size is left to Bedrock. Files: `essera_converse.go`, `adaptor.go`.

## 2026-11 — Playground attachments

The playground's Attach menu attaches files to a message instead of saying the feature is in development.

- **Upload photo**, paste or drop an image: PNG, JPEG, GIF or WebP of up to 3.75 MB, sent as an `image_url` part with a data URL.
- **Upload file:** text and code files (txt, md, csv, json, yaml, html, source code; up to 1 MB) are inlined as a text part headed `File: <name>`; PDF, Word and Excel files (up to 4.5 MB) are sent as an OpenAI `file` part with `file_data`, which the Bedrock adaptor maps to a Converse `document` block (Claude on InvokeModel takes PDF). A message carries up to 10 files and 5 documents; a file over a limit or of another type is left out with a message naming it.
- **Take screenshot** (`getDisplayMedia`) and **Take photo** (`getUserMedia`, with a preview to capture from) are offered only where the browser has them.
- Attachments show as chips that can be removed before sending, and on the sent message. The browser keeps a saved conversation's attachment names but not their data, so after a reload they are no longer sent.
- Whether a model takes images or documents is not known to the web UI; the Gateway's error is shown when it does not.
- **New files:** `web/src/features/essera/playground/*`.

| File | Change |
| --- | --- |
| `web/src/components/ai-elements/prompt-input.tsx` | `filterFiles` lets the caller vet the files a user adds |
| `web/src/features/playground/components/input/playground-input.tsx`, `playground-input-tools.tsx`, `lib/input/input-tool-utils.ts` | The Attach menu, the pending attachments and their limits; the stubs are gone |
| `web/src/features/playground/types.ts`, `lib/message/message-utils.ts`, `lib/message/conversation-message-utils.ts`, `hooks/use-playground-conversation.ts` | A user message carries its attachments into the chat request |
| `web/src/features/playground/components/message/playground-message-content.tsx` | A message shows the files it carried |
| `web/src/features/playground/lib/storage/storage.ts`, `storage-schema.ts`, `__tests__/storage.test.ts` | Saved messages keep attachment names, not data |
| `web/src/i18n/locales/*.json` | The attachment messages |

## 2026-11 — API documentation

- **`essera/apidocs`:** the OpenAPI 3.1 document of the customer API, written by hand in `openapi.yaml`: the five routes an API key may call on a managed gateway (`GET /v1/models`, `POST /v1/chat/completions`, `/v1/responses`, `/v1/messages`, `/v1/embeddings`), authentication, models, limits and billing, every error code, integrations and code samples. It is served as JSON, in the order it is written, at `GET /api/essera/openapi.json` to a signed-in browser session (no access tokens), with the Gateway's public address (`server_address` of `/api/status`) in place of the placeholder and, for one of the Console's people, models they may call in the examples. CI lints it with Redocly (`essera/apidocs/redocly.yaml`).
- **API Docs page:** on a managed gateway, the console sidebar lists API Docs after Audit Logs (`/docs`, signed-in only). The page renders the document with Scalar's API reference, loaded only on that page, in the app's light or dark theme. "Test request" calls the Gateway itself: no Scalar proxy, agent, MCP link, telemetry or remote fonts, and the key typed in is not stored.
- **`scripts/essera/verify_integrations.py`:** makes one tiny request through each integration the page lists (OpenAI Python and JavaScript, Anthropic Python, LangChain, LlamaIndex) against a running gateway and prints PASS or FAIL; clients pinned in `requirements-integrations.txt`. All 12 checks passed against Dev on 2026-11-20.

| File | Change |
| --- | --- |
| `controller/essera.go` | Serves the OpenAPI document |
| `pkg/jsplugin/routing.go` | `/docs` is a console page plugins may not claim |
| `router/api-router.go`, `middleware/access_token_routes.go` | The `/api/essera/openapi.json` route, browser sessions only |
| `web/src/hooks/use-sidebar-data.ts` | The API Docs entry on a managed gateway |
| `web/src/routeTree.gen.ts` | The generated `/docs` route |
| `web/package.json`, `web/bun.lock`, `THIRD-PARTY-LICENSES.md` | `@scalar/api-reference-react` 0.9.88 (MIT) |
| `.github/workflows/backend-pr-validation.yml` | Lints the OpenAPI document |
## 2026-11 — The Essera design (phase A: foundation, shell and landing)

The web UI takes the look of the Essera designs. Pages are restyled one by one in later changes; this one sets what every page inherits.

- **Tokens:** the emerald-on-slate palette in Light and Dark replaces new-api's in the default theme (`web/src/styles/theme.css`), with the designs' radius scale (4, 6, 8, 12, 16 and 24px). Plus Jakarta Sans is the interface font and JetBrains Mono the font of figures, prices, IDs and code, both self-hosted (`@fontsource-variable`, OFL-1.1).
- **Figures:** the `num` utility (mono, tabular, slashed zero, tight) and `NumberValue` (`web/src/features/essera/brand/`), which sets a figure's unit in the emerald accent; the dashboard's stat cards use it.
- **Theme:** Light or Dark, Light by default, for everyone; there is no System choice, and a saved System reads as Light. The top bar has a Light/Dark switch.
- **Managed gateway:**
  - the Essera logo (`EsseraLogo`, an emerald tile with a white "E" and the wordmark "ESSERA / AI Gateway", the Essera Operations Console's mark) in the top bar, the sign-in, the landing and the footer, and `public/essera-favicon.svg` as the favicon, unless `ESSERA_LOGO` names one;
  - the footer shows no new-api tagline; the AGPLv3 §7(b) attribution from `NOTICE` and its link to the new-api repository stay, introduced as one line "Includes open-source software (AGPL-3.0)";
  - no Theme Settings panel (presets, font, radius, density, sidebar style, layout, content width): the design's defaults apply, whatever a browser saved;
  - English only: no language switcher in the top bar, the public header, the setup wizard or the profile; the interface is put in English whatever the browser or a saved preference says, and only the English locale is loaded once the browser knows the gateway is managed;
  - the top bar and sidebar of the designs (56px bar with the managed pill, 240px bordered sidebar, section labels, the emerald active entry); Home is the Overview for someone signed in; the Playground's section is "Explore";
  - the home page is the Essera landing (`web/src/features/essera/landing/`): the hero ("One gateway for every model your teams use") with a sample request in each API format the gateway serves, the figures section (models available and model families counted live from the snapshot, 2 API formats, per-account controls), how it works, and the Console callout. It shows no latency, uptime, volume or region figure, and no third-party chat clients.
- **Translations:** from this change on, new interface strings are added in English only (`web/src/i18n/locales/en.json`). The other locale files stay for upstream merges; a managed gateway does not load them, and elsewhere a missing string falls back to English. `bun run i18n:sync` would copy English into them, so it is not run for Essera strings.

## 2026-11 — The Essera design (phase B: the signed-in pages)

Every signed-in page takes its design, with only real data: widgets of the designs that the Gateway has no data for (compliance badges, latency, SLA, regions, context windows, invented capabilities) are left out. Behaviour and API calls are unchanged.

| File | Change |
| --- | --- |
| `web/src/components/layout/components/section-page-layout.tsx` | The designs' page header: a 24px title, an optional description, and the page padding |
| `web/src/components/ui/table.tsx`, `web/src/components/data-table/core/data-table-view.tsx`, `core/pagination.tsx`, `core/column-pinning.ts`, `layout/data-table-page.tsx` | Tables on a card (12px radius, card shadow), 13px cells, 12px semibold headers, mono cells keep their font; on desktop the pagination sits inside the card, total on the left, and the card is only as tall as its rows; pinned cells on the card surface |
| `web/src/components/status-badge.tsx` | A badge is a pill with a tinted background and border in its own colour |
| `web/src/components/ui/empty.tsx` | Empty states have an emerald icon tile and a bold title |
| `web/src/features/keys/index.tsx`, `components/api-keys-cells.tsx` | API Keys: a description; the masked key in mono |
| `web/src/features/usage-logs/index.tsx`, `components/columns/common-logs-columns.tsx`, `components/model-badge.tsx`, `components/log-cost-display.tsx` | Usage Logs: a description; time on two mono lines with a pill type tag; mono model, tokens and cost; column widths that fit 1366px (Details was cut off) |
| `web/src/features/usage-logs/audit/index.tsx`, `components/audit-log-columns.tsx` | Audit Logs: a description; mono time, IP, method, route and status; column widths that fit 1366px |
| `web/src/features/dashboard/components/overview/overview-dashboard.tsx`, `summary-cards.tsx`, `ui/panel-wrapper.tsx` | Overview: on a managed gateway the setup guide is collapsed behind a "Setup guide" button; the designs' cards; content panels in one row of equal height |
| `web/src/features/dashboard/index.tsx`, `components/models/log-stat-cards.tsx`, `model-charts.tsx`, `consumption-distribution-chart.tsx`, `components/users/user-charts.tsx`, `lib/charts.ts` | Dashboard: one card per figure, the last highlighted; tabs with an emerald active edge; the designs' series colours and chart theme (mono axes and legend, dashed grid) |
| `web/src/features/profile/index.tsx`, `components/profile-header.tsx`, `components/sidebar-modules-card.tsx` | Profile: round avatar, the person's Gateway role on a managed gateway, figures with icon tiles; the sidebar settings at full width in two columns (the empty left column is gone) |
| `web/src/features/security/index.tsx`, `components/login-sessions-card.tsx`, `components/login-session-item.tsx` | Security & Access: a description, the sessions card at the page's width, the Current pill, mono IP and method, an outlined Revoke |
| `web/src/features/playground/index.tsx`, `components/chat/playground-empty-state.tsx`, `components/input/playground-input*.tsx` | Playground: the page header, the chat framed in a card, the starter cards with a description, the composer card with a mono model chip and the primary Send; Attach and the hidden Search unchanged |
| `web/src/features/pricing/index.tsx`, `components/pricing-toolbar.tsx`, `components/model-card.tsx` | Model Square: a left-aligned header with an "N models" pill, search in the toolbar card; on a managed gateway each model is an `EsseraModelCard` |
| `web/src/features/auth/sign-in/index.tsx` | On a managed gateway the Essera form carries its own heading |
| `web/src/features/errors/*.tsx` | The designs' error page (404, 401, 403, 500, 503): mono status, Overview or Home, Model Square and Go Back; on a managed gateway no link to new-api's issue tracker; the 503 page's inert "Learn more" is gone |

New Essera files: `web/src/features/essera/brand/chart-theme.ts`, `web/src/features/essera/error-page.tsx`, `web/src/features/essera/model-square-card.tsx`. Essera's own sign-in (8 code boxes; a complete code is sent once, and Verify is disabled while it is checked), key-created dialog, Users page and API Docs page (Scalar themed to the tokens, a View API Keys link) are restyled in place.

## 2026-11 — A key's state from the Console, and the client address behind Cloudflare

- **`essera/keys` (`Project`):** every applied configuration, and the master's start-up, brings the token row of each key not deleted on the Gateway to what relay makes of it: active while the snapshot publishes it, expired once its expiry has passed, and revoked (token status 5, `keys.TokenStatusRevoked`) once the snapshot no longer publishes it. The expiry the snapshot publishes becomes the row's. A key the Console has not published yet keeps its row for 10 minutes, so a version written before the Console heard of it revokes nothing; `essera_keys.published_at` records the first version that listed it. The snapshot lists active keys only and carries no revocation time or reason, so neither is shown.
- **API keys page** in managed mode (`web/src/features/essera/key-status.ts`): a key reads Active, Expired or Revoked, filters by those states, and an active key whose expiry has passed since the last version reads Expired.
- **`essera/clientip`:** with `ESSERA_CLIENT_IP_HEADER` set (`CF-Connecting-IP` behind Cloudflare), a request whose peer is a trusted proxy (`TRUSTED_PROXIES`) and that carries one valid address in that header is served as coming from that address, so login sessions, audit, sign-in throttling and rate limits record the person rather than the load balancer or a Cloudflare edge. The header from any other peer is ignored. Unset, nothing changes.

| File | Change |
| --- | --- |
| `controller/token.go` | On a managed gateway, `PUT /api/token` is refused (409) in the handler too, so a key the Console revoked or that expired is never enabled again |
| `main.go` | Serves through `essera/clientip` |
| `web/src/features/keys/components/api-keys-columns.tsx`, `web/src/features/keys/components/api-keys-table.tsx`, `web/src/routes/_authenticated/keys/index.tsx`, `web/src/i18n/locales/en.json` | A managed key's state and its filter |
