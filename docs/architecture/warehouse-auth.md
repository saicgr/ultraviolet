# Warehouse Auth — Per-Warehouse Modes

Auth is per-connection, not per-customer. A customer can have multiple connections, each with its own auth config. Stored encrypted (AES-256-GCM) in `connections.credentials_encrypted`.

## Snowflake

| Mode | When | Field shape |
|---|---|---|
| Username + password | Smaller accounts; dev | `{ "user": "...", "password": "..." }` |
| Username + JWT (PKCS8 keypair) | Enterprise — most production accounts | `{ "user": "...", "private_key_pem": "<pkcs8>", "private_key_passphrase": "..." }` |
| OAuth (Snowflake OAuth integration) | Federated identity | `{ "oauth_client_id": "...", "oauth_client_secret": "...", "oauth_token_endpoint": "..." }` |
| External browser (SSO) | NOT supported — interactive only | n/a |

Notes:
- JWT-PKCS8 is the recommended production mode (per Snowflake security guidance + enterprise customer requirements).
- Many enterprise Snowflake accounts disable password auth entirely — JWT-PKCS8 is mandatory.
- gosnowflake handles all three modes natively.
- Token refresh: gosnowflake handles automatically; UV sets a long-running session.

## BigQuery

| Mode | When | Field shape |
|---|---|---|
| Service Account JSON | Default — works in any GCP project | `{ "service_account_json": "<full-json>" }` |
| OAuth2 user credentials | When customer can't create service accounts | `{ "oauth_refresh_token": "...", "oauth_client_id": "...", "oauth_client_secret": "..." }` |
| Workload Identity Federation (WIF) | Cross-cloud — UV running on GCP/AWS proves identity to BQ | `{ "wif_provider": "...", "wif_audience": "...", "wif_subject_token_type": "..." }` |
| Application Default Credentials (ADC) | UV process running on GCP with attached SA | `{ "adc": true }` (no creds stored) |

Recommended:
- For UV-managed customers: WIF — no long-lived secret to rotate.
- For BYOC / self-hosted: service-account JSON.

`cloud.google.com/go/bigquery` supports all four via `option.WithCredentialsJSON`, `option.WithTokenSource`, `option.WithCredentialsFile`, etc.

## Databricks (Phase 2)

| Mode | When | Field shape |
|---|---|---|
| Personal Access Token (PAT) | Quick start, dev | `{ "host": "https://...", "token": "dapi..." }` |
| OAuth M2M (service principal) | Production | `{ "host": "...", "client_id": "...", "client_secret": "..." }` |
| OAuth U2M | Interactive — N/A for backend | not supported |

OAuth M2M is the production-recommended mode (replaces PATs over time per Databricks roadmap).

## Encryption at rest

All credential blobs are JSON-marshaled, then encrypted with AES-256-GCM.
Key: HKDF-derived from `ENCRYPTION_KEY` env using `customer_id` as salt.
Nonce: random per encryption; stored alongside ciphertext.

`internal/store/crypto.go` is the single point — no other module touches plaintext credentials except in the connector constructor (which decrypts in-memory and passes the SDK its expected shape).

## Connection test

`POST /api/v1/connections/:id/test`:
1. Decrypt creds.
2. Construct connector.
3. Run `SELECT 1` (or `SELECT current_database()` for SF).
4. Return success/failure + warehouse-reported version.
5. Never persist plaintext anywhere; clear in-memory after test.

## Rotation

Replace credential by editing the connection in UI:
1. UV re-encrypts new value.
2. Old connector instance closed; new constructed.
3. In-flight queries on the old connector complete; new ones use new creds.

Forward-compat: storing `credential_version` int alongside ciphertext so future formats can coexist.

## Phase 1 minimum

- BigQuery: service-account JSON + WIF.
- Snowflake: username/password + JWT-PKCS8.
- Databricks: deferred.
