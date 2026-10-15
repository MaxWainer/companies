# Companies service

## Requirements

- Docker with Compose v2
- `make`, `curl` and `openssl`
- Go 1.27.2

## Run everything in Docker

Useful urls:
| What             | Default URL                           |
|------------------|---------------------------------------|
| API              | http://localhost:8080/v1/companies    |
| Swagger UI       | http://localhost:8080/docs/index.html |
| Grafana          | http://localhost:3000                 |
| Redpanda Console | http://localhost:8081                 |
| pgAdmin          | http://localhost:5050                 |

Bash commands:
```bash
make start   # start everything
make info    # print the links and a fresh token again
make stop    # stop everything; data is kept
make clean   # stop everything and delete the data
```

## Use the API

Reads are public. Creating, changing and deleting a company needs a bearer
token:

```bash
export TOKEN=$(make -s token SUB=alice)
```

```bash
curl -s -X POST http://localhost:8080/v1/companies \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"Acme","amount_of_employees":42,"registered":true,"type":"CORPORATIONS"}'
```

```bash
curl -s http://localhost:8080/v1/companies/<id>
```

```bash
curl -s -X PATCH http://localhost:8080/v1/companies/<id> \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/merge-patch+json" \
  -d '{"amount_of_employees":50}'
```

```bash
curl -s -X DELETE http://localhost:8080/v1/companies/<id> -H "Authorization: Bearer $TOKEN"
```

`type` is one of `CORPORATIONS`, `NON_PROFIT`, `COOPERATIVE` or
`SOLE_PROPRIETORSHIP`. Swagger UI documents every field and error; click
**Authorize** and paste `Bearer <token>` to call the protected endpoints
from there.

To see the events, open the `companies.events.v1` topic in Redpanda Console,
or run `make events`.

## Run the service on your machine

```bash
make deps   # PostgreSQL, Redpanda and the tools in Docker
make run    # the service, configured from .env.example
```

## Tests

```bash
make test               # unit tests
make test-integration   # against real PostgreSQL and Redpanda in Docker
make lint
make postman            # the Postman collection against a running `make start` stack
```

`make help` lists the other targets.
