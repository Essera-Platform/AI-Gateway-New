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
  - monthly request, token and spend limits. A request is reserved against the request quotas when the relay takes it, so concurrent requests cannot all take the last one, and given back if it reaches no provider; tokens and spend are added when it ends, whatever its outcome, as the Console counts them. Requests and tokens are one count per subject whatever the currency; spend is kept by currency.
- **`essera/providerlimits`:** applies each provider's settings from the snapshot to every attempt:
  - the rate limit in requests per minute, counted across nodes in the database, with 429 `essera_provider_rate_limited` past it;
  - the provider's retries as the request's retry budget;
  - its timeout as the time the provider has to start answering.
- **`essera/rating`:** prices a request with the snapshot's rates, as the Console does, for spend limits and the rate versions on usage events.
- **`essera/usage`:** one `usage.recorded` event per request a customer had served, sent to the usage queue through a database outbox (events SQS keeps refusing are set aside after 20 attempts, so they never hold back the rest). Images are counted as settled, audio in minutes of sound.
- **`essera/stats` and `essera/bridge`:** request statistics, and the internal API the Console's backend reads (`/internal/essera/v1`: state and active configuration version, a provider's connection test, a provider's telemetry, and the traffic of the last hour, day or week: requests, latency, HTTP statuses, denials and each provider's availability, latency, retries and timeouts). It is reached inside the cluster only.
- **`essera/servicetrust`:** authenticates the Console's backend on the internal API by its AWS role. The backend sends an `sts:GetCallerIdentity` request signed by its task role; the Gateway forwards it to STS (its global or a regional endpoint, nothing else) and serves only the ARNs in `ESSERA_TRUSTED_PRINCIPALS`. A header named twice is refused, and a verified identity is not reused past its signature's lifetime. No secret is shared.
- **`essera/bootstrap`:** sets up the service user and token that customer requests run as. Its group's ratio is zero, so new-api neither reserves nor charges quota; the Console bills. It turns off new-api's consume log, which would duplicate the Console's ledger, and replaces the default root password.
- **`essera/managed`:** in managed mode, refuses local edits of what the Console owns (channels, users, tokens, prices, models) and of plugins and sign-in providers, and closes the playground, Midjourney, Suno, the plugins' task routes (`/v1/tasks`) and video remix. Customers may only POST to the synchronous relay routes (not Gemini's long-running operations) and GET the model lists; a GET on `/v1/responses` would open a WebSocket whose calls are not accounted for. The video routes are open to customers for Bedrock video models only: `POST /v1/video/generations` and `POST /v1/videos` for a model Bedrock serves by asynchronous invocation, and `GET /v1/video/generations/:id`, `/v1/videos/:id` and `/v1/videos/:id/content` for the customer's own tasks (every customer runs as one new-api user, so the Gateway checks the task's customer itself).
- **Bedrock video (`relay/channel/aws/async_video.go`, `essera/usage/tasks.go`):** a native Go task adaptor starts Nova Reel and Luma Ray videos with `StartAsyncInvoke`, writing to `s3://$BEDROCK_OUTPUT_BUCKET/<task>/`, and polls them with `GetAsyncInvoke`. A finished video is read through a GET URL presigned for 15 minutes on every fetch; the task stores none. Bedrock is called with the task role (an API key cannot write to S3), or with the channel's access key when it holds one. A customer's task submission goes through the same policy, request quota, provider rate limit and retries as a relayed request; its caller is kept in `essera_tasks`, and when the task ends one `usage.recorded` event reports it: modality `video`, `generated_units` the video's seconds, or `failed` with no units. A submission the provider refused is reported failed at once; one that never reached it gives its reservation back.
- **`web/src/features/essera`:** in managed mode, a badge in the admin header says the Console administers the gateway, and its tooltip says what is changed there.

### Upstream files changed

| File | Change |
| --- | --- |
| `main.go` | Starts the integration before the node sync; installs the managed-mode guard |
| `middleware/auth.go` | `TokenAuth` hands a customer's access token to the integration before the key is split, and narrows the service token's models to the customer's |
| `middleware/essera.go` (new) | The customer authentication and policy middleware |
| `middleware/utils.go` | Counts a customer's request that new-api refuses after the Essera policy (a model outside the entitlements, no channel), which never reaches relay |
| `controller/relay.go` | Reports every finished relay request to the integration; reserves a customer's request against its quotas; admits each attempt against the provider's rate limit and takes the provider's retries as the retry budget. Does the same for task submissions, and hands each one's outcome to the integration |
| `relay/relay_adaptor.go` | A task on an AWS channel runs on the native Bedrock video adaptor; `PresignTaskResult` signs a finished task's URL per read |
| `relay/relay_task.go`, `controller/video_proxy.go` | A task fetch and the video proxy read a Bedrock video through a freshly presigned URL |
| `service/task_polling.go` | `TaskFinishedFunc` receives every task that reaches its final state, including tasks failed in bulk |
| `relay/channel/aws/async_video.go` (new) | The Bedrock asynchronous video adaptor |
| `relay/channel/api_request.go` | Bounds the wait for a provider's answer by the provider's timeout |
| `controller/essera.go` (new) | Runs the Console's connection tests through new-api's channel test |
| `router/main.go` | Registers the internal API |
| `controller/misc.go` | `/api/status` says whether the gateway is managed (`essera_managed`) |
| `web/src/components/layout/components/app-header.tsx` | Shows the managed badge |
| `model/channel.go` | `GetNextEnabledKey` resolves a key that is a Secrets Manager reference |
| `model/essera_hooks.go` (new) | The hook that resolution goes through |
| `dto/channel_constraints.go`, `model/channel_constraint.go` | A channel filter that limits a model to the providers a customer may use |
| `web/src/components/layout/components/footer.tsx`, `web/src/i18n/locales/*.json` | The footer shows the AGPLv3 §7(b) notice from `NOTICE` verbatim; the locales also carry the managed badge's texts |
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
| `BEDROCK_OUTPUT_BUCKET` | The bucket Bedrock writes generated videos to, in the provider's region; the task role needs `bedrock:StartAsyncInvoke`, `bedrock:GetAsyncInvoke`, and `s3:PutObject` and `s3:GetObject` on it |
| `ESSERA_AWS_ENDPOINT`, `ESSERA_JWKS_URL`, `ESSERA_PROVIDER_BASE_URLS` | Development only: local stand-ins for AWS, Cognito and providers |

### Database

The fork runs on PostgreSQL only. The Essera tables are:

- `essera_config_versions`
- `essera_usage_outbox`
- `essera_usage_counters`
- `essera_provider_minutes`
- `essera_request_stats`
- `essera_provider_errors`
- `essera_tasks`

They hold working state; every business record stays in the Console.
