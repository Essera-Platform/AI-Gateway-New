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
- **`essera/auth`:** authenticates customers by the Cognito client-credentials access tokens of the Console's customer pool (`ESSERA_COGNITO_USER_POOL_ID`, scopes `ESSERA_CUSTOMER_SCOPES`; with no scopes configured every token is refused). The `client_id` must be an unexpired credential of an active customer in the active snapshot. The pool's signing keys are fetched at most once a minute, so tokens naming unknown keys cannot make the Gateway call Cognito on every request. In managed mode, relay paths accept only these tokens; a new-api key is refused.
- **`essera/policy` and `essera/limits`:** enforce the snapshot before routing:
  - maintenance, including a scheduled window once it has begun;
  - partner, customer and end-user status;
  - approved and blocked countries;
  - models and providers;
  - monthly request, token and spend limits, checked against the totals the Console publishes from its ledger (`usage.totals` on the configuration queue, kept in the database). Spend is the Console's figure only; the Gateway prices nothing. Requests and tokens are the Console's total plus what the Gateway counted from the totals' `as_of` on: a request is reserved when the relay takes it, so concurrent requests cannot all take the last one, and given back if it reaches no provider; tokens are added when it ends, whatever its outcome. Until totals arrive, limits use zero spend and the Gateway's own counts, and this is logged.
- **`essera/providerlimits`:** applies each provider's settings from the snapshot to every attempt:
  - the rate limit in requests per minute, counted across nodes in the database, with 429 `essera_provider_rate_limited` past it;
  - the provider's retries as the request's retry budget;
  - its timeout as the time the provider has to start answering.
- **`essera/usage`:** one `usage.recorded` event per request a customer had served, sent to the usage queue through a database outbox (events SQS keeps refusing are set aside after 20 attempts, so they never hold back the rest). Images are counted as settled, audio in minutes of sound. The event measures and names no rate version; the Console rates it.
- **`essera/stats` and `essera/bridge`:** request statistics, and the internal API the Console's backend reads (`/internal/essera/v1`: state and active configuration version, a provider's connection test, a provider's telemetry, and the traffic of the last hour, day or week: requests, latency, HTTP statuses, denials and each provider's availability, latency, retries and timeouts). It is reached inside the cluster only.
- **`essera/servicetrust`:** authenticates the Console's backend on the internal API by its AWS role. The backend sends an `sts:GetCallerIdentity` request signed by its task role; the Gateway forwards it to STS (its global or a regional endpoint, nothing else) and serves only the ARNs in `ESSERA_TRUSTED_PRINCIPALS`. A header named twice is refused, and a verified identity is not reused past its signature's lifetime. No secret is shared.
- **`essera/bootstrap`:** sets up the service user and token that customer requests run as. Its group's ratio is zero, so new-api neither reserves nor charges quota; the Console bills. It turns off new-api's consume log, which would duplicate the Console's ledger, and replaces the default root password.
- **`essera/managed`:** in managed mode, refuses local edits of what the Console owns (channels, users, tokens, prices, models) and of plugins and sign-in providers, closing an account, and new-api's OAuth sign-in, and closes the Midjourney, Suno, video and task routes. A person may create and delete their own keys. Customers may only POST to the synchronous relay routes (not Gemini's long-running operations) and GET the model lists; a GET on `/v1/responses` would open a WebSocket whose calls are not accounted for.
- **`essera/configsync` people:** each person the snapshot publishes (`accounts.people`) becomes one new-api user, linked by `oidc_id` = Cognito sub and the `essera_people` table, with new-api's common-user role and the person's Gateway role, `admin` or `user`, in `essera_people`; an admin is never a new-api administrator. Nothing else of the person is kept. A person removed or suspended in the Console is disabled at the next version, and their sessions and Cognito sign-ins end at once.
- **`essera/identity`:** the Gateway's own sign-in for people, through the Cognito user-pool API of the customer pool (no hosted page, no redirect): `InitiateAuth` `USER_AUTH`, then `RespondToAuthChallenge` with the emailed code (`ESSERA_SIGN_IN=email_otp`, the default) or the password (`password`), and a new password on a first password sign-in. The Cognito session between two steps stays on the server, sealed, behind an HttpOnly cookie, and each step is spent when used. Every wrong email, password or code, unknown or disabled user and inactive person gets the same answer; an unknown email is asked for a code too. Steps are throttled per email and per IP across nodes. The ID token is verified against the pool's keys, issuer, app client and use, and its sub must be an active person. The Cognito refresh token is kept sealed on the server with the Gateway session; each session refresh renews it (`GetTokensFromRefreshToken`, or `REFRESH_TOKEN_AUTH` without rotation) and ends the person's sessions when Cognito refuses it or the person is no longer active; signing out revokes it (`RevokeToken`). Operators (root) keep new-api's password sign-in; nobody else may use it.
- **`essera/keys`:** a person creates their own `sk-` keys (90 days unless shorter is chosen) and deletes them. The Gateway keeps only the key's id; the key is shown once. `credential.created` and `credential.revoked` go to the Console on the usage queue through the outbox, with a `type`. On relay a key is accepted only while the active snapshot publishes it as an `api_key` credential with its id and SHA-256, unexpired, of an active person of the same customer, and it is not deleted on the Gateway; it then runs as that customer and, for role `user`, that end user, through the same policy, limits, usage and rating as a Cognito token, with the key id as `credential_ref`.
- **`essera/consoleapi`:** reads the Console's internal API (`/internal/v1` at `CONSOLE_INTERNAL_URL`) for a signed-in person: their account, usage, usage totals, audit, prices and the tenant's people, and asks the Console to add, suspend, reactivate or remove an end user. Each call proves the Gateway's task role with a signed `sts:GetCallerIdentity` for the audience `essera-console-<env>` (`essera/servicetrust.Identity`, the Console's own mechanism the other way round) and names the person in `X-Essera-Person`. When the Console does not answer, the pages say the data is temporarily unavailable; relay never depends on it.
- **Pages of the Console's people:** usage logs, their totals, the dashboard series, audit and the account summary (spend, limits and what is left) come from the Console in new-api's response formats; the Gateway calculates no money. The Model Square shows a signed-in person only the models relay lets them call (`policy.Available`), with the prices the Console gives for them, and a person who is not active nothing; a guest sees the snapshot's active models without prices. An admin sees their customer's people on the Users page and adds, suspends, reactivates and removes users there; the Gateway checks that the caller is an active admin and the target a user (role `user`) of the same customer, never an admin or themselves, before the Console does the same. Operators keep new-api's own pages.
- **Playground:** runs as the signed-in person, through the same policy, limits and usage as their API calls; anyone else is refused.
- **Model lists:** a person's model list (`/api/user/models`, used by the playground) holds only what relay lets them call (`policy.Available`, the rules `policy.Evaluate` applies): active models of active providers within the customer's entitlements and, for a user, their own limits; nothing while they are not active.
- **Groups:** everyone is in group `essera`; group choices are hidden.
- **`web/src/features/essera`:** in managed mode, the Essera sign-in (email, then the emailed code with resend and expiry, or the password and a first new password), with a link to the operators' sign-in; creating a key with only a name and an expiry and showing it once; and a badge in the admin header says the Console administers the gateway, and its tooltip says what is changed there.

### Upstream files changed

| File | Change |
| --- | --- |
| `main.go` | Starts the integration before the node sync; installs the managed-mode guard |
| `middleware/auth.go` | `TokenAuth` hands a customer's access token to the integration before the key is split, and narrows the service token's models to the customer's |
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
| `router/api-router.go`, `router/access_token_scope_test.go` | The anonymous `/api/essera/sign-in` routes |
| `router/relay-router.go` | The playground runs through the Essera policy |
| `service/group.go` | A managed gateway offers only the `essera` group |
| `web/src/features/auth/sign-in/index.tsx` | The Essera sign-in in managed mode |
| `web/src/features/keys/components/*` | In managed mode, the key is shown once; edit, enable, reveal, quota and group are hidden |
| `web/src/components/model-group-selector.tsx`, `web/src/features/playground/components/input/playground-input-controls.tsx` | The playground hides the group choice in managed mode |
| `controller/essera_pages.go` (new) | The pages of the Console's people, the tenant's people and the Model Square |
| `controller/log.go`, `controller/usedata.go`, `controller/access_token.go`, `controller/user.go`, `controller/pricing.go` | A person's usage logs, dashboard, audit and account summary, and the managed Model Square, come from the Console |
| `router/api-router.go`, `middleware/access_token_routes.go` | The admin's `/api/essera/people` routes, browser sessions only |
| `essera/awsx/awsx.go` | The task role's credentials, to sign the Gateway's identity |
| `web/src/features/essera/*`, `web/src/features/pricing/*`, `web/src/features/usage-logs/*`, `web/src/features/dashboard/*`, `web/src/features/profile/*`, `web/src/hooks/use-sidebar-*.ts`, `web/src/routes/_authenticated/route.tsx`, `web/src/routes/_authenticated/users/index.tsx`, `web/src/components/layout/components/mobile-drawer.tsx`, `web/src/stores/auth-store.ts` | In managed mode: the Console's figures, the tenant's people for its admin, the Model Square's models and prices, and the menus and pages the Console owns closed |
| `go.mod`, `go.sum`, `THIRD-PARTY-LICENSES.md` | AWS SDK modules for S3, SQS, Secrets Manager and shared configuration, and go-jose for the customer pool's signing keys (Apache-2.0) |

### Settings

| Variable | Meaning |
| --- | --- |
| `ESSERA_MANAGED` | `true` turns the integration on |
| `ESSERA_ENVIRONMENT` | `dev`, `uat` or `prod`; names the queues (`essera-{env}-config-updates`, `essera-{env}-usage-events`) |
| `ESSERA_COGNITO_USER_POOL_ID`, `ESSERA_CUSTOMER_SCOPES` | The Console's customer pool and the scopes its credentials carry; the Console's own names `CUSTOMER_USER_POOL_ID` and `CUSTOMER_OAUTH_SCOPES` are read when these are unset |
| `ESSERA_TRUSTED_PRINCIPALS` | IAM ARN patterns allowed to call the internal API, e.g. `arn:aws:sts::<account>:assumed-role/essera-dev-task/*` |
| `ESSERA_PROVIDER_TYPES` | Optional adapter per provider key, `{"acme-llm": 1}`, for keys the defaults do not recognise |
| `ESSERA_FALLBACK_POLICIES` | Optional, `{"fallback/openai-to-azure": ["azure-openai"]}` |
| `ESSERA_OIDC_ISSUER`, `ESSERA_OIDC_CLIENT_ID`, `ESSERA_OIDC_CLIENT_SECRET` | The customer pool (`https://cognito-idp.<region>.amazonaws.com/<pool id>`) and the app client people sign in with |
| `CONSOLE_INTERNAL_URL` | The Console's internal API inside the cluster, e.g. `http://essera-backend:8000` |
| `ESSERA_SIGN_IN` | `email_otp` (default; the pool must send email through SES) or `password` |
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
