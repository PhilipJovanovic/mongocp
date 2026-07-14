# mongocp

A small HTTP control plane for MongoDB, built to be used as a **Custom GPT Action** in ChatGPT (the successor to the discontinued plugin system). ChatGPT can create collections, read/write documents, and run aggregation pipelines against your database.

## Features

- List / create / drop collections
- Insert, query, update, and delete documents with plain MongoDB filters
- Run aggregation pipelines for analytics (`$out` / `$merge` are blocked)
- Bearer-token auth, one token per instance
- Serves its own OpenAPI 3.1 spec at `/openapi.json` for direct import into a GPT
- ObjectIDs are returned as plain hex strings and accepted back as strings in `_id` filters, so the model never has to deal with `$oid`

## Configuration

Everything is configured through environment variables (see `.env.example`):

| Variable | Required | Description |
| --- | --- | --- |
| `MONGO_URI` | yes | Connection string (`mongodb://` or `mongodb+srv://`) |
| `MONGO_DB` | yes | Database this instance operates on |
| `API_TOKEN` | yes | Bearer token clients must present (`openssl rand -hex 32`) |
| `PUBLIC_URL` | no | Public base URL, embedded in the OpenAPI spec |
| `PORT` | no | HTTP port, default `8080` |

One instance = one database + one token. To expose several databases, deploy the service multiple times with different env values — on Coolify that is just another application from the same repo.

## Run locally

```sh
docker compose up --build
# or, with a Mongo already running:
API_TOKEN=dev-token MONGO_URI=mongodb://localhost:27017 go run .
```

Smoke test:

```sh
curl localhost:8080/health
curl -H "Authorization: Bearer dev-token" localhost:8080/collections
```

## Deploy on Coolify

1. Push this repo to Git and add it in Coolify as a new application (build pack: **Dockerfile**).
2. Set the env vars `MONGO_URI`, `MONGO_DB`, `API_TOKEN`, and `PUBLIC_URL` (the domain Coolify assigns, e.g. `https://mongocp.your-domain.com`).
3. Set the exposed port to `8080` and deploy. Coolify terminates TLS for you — never run this without HTTPS.
4. Health check endpoint: `GET /health`.

If MongoDB also runs on Coolify, prefer the internal network hostname in `MONGO_URI` so the database is not exposed publicly.

## Connect to ChatGPT

1. In ChatGPT: **Explore GPTs → Create → Configure → Create new action**.
2. Import the schema from `https://<your-domain>/openapi.json`.
3. Authentication: **API Key** → Auth type **Bearer** → paste your `API_TOKEN`.
4. Ask the GPT things like *"create a collection called customers"*, *"insert these three orders"*, or *"group orders by status and sum the totals"*.

## API

All endpoints except `/health` and `/openapi.json` require `Authorization: Bearer <API_TOKEN>`.

Every operation is a flat POST with the target collection in the JSON body — no path parameters. (The GPT Actions importer tends to drop request bodies on operations that mix path parameters with a body.)

| Method | Path | Body |
| --- | --- | --- |
| GET | `/collections` | – |
| POST | `/collections/create` | `{"collection": "...", "validator": {...}?}` |
| POST | `/collections/drop` | `{"collection": "..."}` |
| POST | `/documents/insert` | `{"collection": "...", "documents": [{...}]}` |
| POST | `/documents/query` | `{"collection": "...", "filter": {}?, "projection": {}?, "sort": {}?, "limit": 50?, "skip": 0?}` |
| POST | `/documents/update` | `{"collection": "...", "filter": {}, "update": {}, "many": false?, "upsert": false?}` |
| POST | `/documents/delete` | `{"collection": "...", "filter": {}, "many": false?}` |
| POST | `/documents/aggregate` | `{"collection": "...", "pipeline": [{...}]}` |

Notes:

- Query results are capped at 1000 documents per call (default 50); use `skip` to paginate.
- `update` with a plain field map is applied as `$set`; documents with `$` operators are passed through unchanged.
- `update` and `delete` refuse an empty filter unless `many: true` is set explicitly, so the model cannot wipe a collection by accident.

## Security notes

- The token holder has full read/write access to the configured database — treat the `API_TOKEN` like a database password and rotate it if it leaks.
- Give the MongoDB user in `MONGO_URI` `readWrite` on the one database only, not admin/cluster roles; that limits the blast radius and is what actually prevents cross-database access.
- Request bodies are capped at 5 MiB and every operation has a 30 s server-side timeout.
- Possible later evolution: a master database that maps API keys to tenant databases/collections, so one deployment serves many GPTs. The per-instance env model keeps the same API surface, so that can be added without breaking clients.
