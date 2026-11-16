# CLAUDE.md — Project Conventions for new-api

## MANDATORY: Read AGENTS.md with the Read tool

Do not treat `@AGENTS.md` as loaded. Claude Code does not reliably inline that import.

Before any planning, coding, reviewing, or answering a project question, you MUST call the Read tool on the repo-root file `AGENTS.md` and wait for the full contents. This is the first action of every session and every new task.

Rules:

- Do not start from memory, summaries, or this file alone.
- Do not skip the Read because a previous turn mentioned AGENTS.md.
- Do not replace the Read with a grep, glob, or partial skim.
- After reading, follow every rule in `AGENTS.md` for the rest of the work.
- If the task touches `web/`, also Read `web/AGENTS.md` before editing frontend files.
- If the task touches billing as defined under **Billing rules (mandatory read gate)** in `AGENTS.md`, also Read `.agents/rules/billing.md` in full before planning or editing. Tasks outside that definition may skip it.

## Essera fork: license rules (mandatory)

This repository is Essera's fork of new-api, which is licensed under the GNU AGPL v3 with additional terms under Section 7 (`LICENSE`, `NOTICE`, `README.md` → License). Every change made in this fork is bound by them. These rules apply on top of `AGENTS.md`; nothing here is legal advice. A licensing question that these rules do not settle goes to the user before any code is written.

- **The license stays.** Never remove, shorten, or replace `LICENSE`, `NOTICE`, or `THIRD-PARTY-LICENSES.md`, or the license section of `README.md`. Docker images and frontend bundles that this fork builds must carry `LICENSE` and `NOTICE`. Do not add a different license header to any file.
- **Attribution in the UI (AGPLv3 §7(b)).** Every user interface this fork serves must keep the following in a prominent about, legal, footer, or attribution location:
  - the sentence `Frontend design and development by New API contributors.`;
  - a visible link to https://github.com/QuantumNous/new-api.

  Essera branding may be added next to them, never instead of them. The protected new-api and QuantumNous identifiers in `AGENTS.md` → Project Governance stay untouched.
- **Mark modifications (AGPLv3 §5(a), §7(c)).** Do not misrepresent the origin of the software, and keep Essera's changes identifiable:
  - **Files changed from upstream:** add a short notice at the top of each one, for example `// Modified by Essera, <yyyy-mm>: <what changed>`.
  - **New Essera files:** put them in clearly named Essera paths where possible (for example `essera/`, `service/essera/`, `web/src/features/essera/`).
  - **Record:** list Essera's changes in `CHANGES-ESSERA.md` at the repository root, created with the first such change.
- **Network use means source must be offered (AGPLv3 §13).** Customers calling the API and administrators using the web UI all interact with this program over a network. Each of them must be offered the Corresponding Source of the version actually deployed, for example through a source link in the UI's about or footer location and on a public endpoint.
  - Treat everything committed here, including the Essera integration code, as code that will be offered under the AGPL.
  - Never put secrets, customer data, or Essera-confidential business logic in this repository. Secrets come from AWS Secrets Manager and the environment at runtime.
- **Keep the Console separate.** The Essera Operations Console (`Essera-Platform/essera-monorepo`) is a separate program that talks to the Gateway only at arm's length: the configuration snapshot in S3 plus the SQS `config-updates` queue, usage events on SQS `usage-events`, and the Gateway's internal HTTP endpoints.
  - Never copy code between the two repositories in either direction.
  - Never import one from the other, and never link them into a single binary.
  - Contracts are shared as data formats (JSON Schema in `docs/handoff/event-contracts/` of the monorepo), not as code.
- **Dependencies must be compatible.** A new direct dependency must have an AGPLv3-compatible license, such as MIT, BSD, Apache-2.0, MPL-2.0, LGPL, GPLv3 or AGPLv3.
  - Do not add proprietary, "source-available", SSPL, Commons Clause, or GPLv2-only code.
  - Add every new direct Go or `web/` dependency to `THIRD-PARTY-LICENSES.md`. Add its Apache-2.0 NOTICE text, if it has one, to `NOTICE`.
- **Upstream code.** Pulling changes from `upstream` keeps their license and notices as they are. Code from any other project may be copied in only if its license is compatible, and its copyright notice must stay with it.
- **`electron/` is out of scope** for Essera: do not change, build, or ship it.

## Essera fork: working rules

- **Database: PostgreSQL only.** The fork runs on PostgreSQL. The three-database verification in `AGENTS.md` (SQLite, MySQL, PostgreSQL) applies only to changes contributed upstream. Essera code is tested on PostgreSQL through `ESSERA_TEST_POSTGRES_DSN` (`essera/records/recordstest`), and on SQLite when that is unset.
- **Upstream contribution rules.** The Issue and Pull request rules in `AGENTS.md` → Project Governance govern contributions to new-api. They do not apply to the fork's own pull requests.
- **Where Essera code lives.** Integration code goes in `essera/`. Upstream files get only short hooks marked `Modified by Essera, <yyyy-mm>`, each listed in `CHANGES-ESSERA.md`.
