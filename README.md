# Companies service

## Requirements

- Docker with Compose v2
- `make`, `curl` and `openssl`
- Go 1.27.2

## Running

Useful urls:
- API: http://localhost:8080/v1/companies
- Swagger UI: http://localhost:8080/docs/index.html
- Grafana: http://localhost:3000
- Redpanda Console: http://localhost:8081
- pgAdmin: http://localhost:5050

Bash commands:
```bash
make start   # start everything
make info    # print the links and a fresh token
make stop    # stop everything
make clean   # stop everything and delete the data
```

## Use the API (via cURL)

Generate token:
```bash
export TOKEN=$(make -s token SUB=alice)
```

Create company:
```bash
curl -s -X POST http://localhost:8080/v1/companies \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"Acme","amount_of_employees":42,"registered":true,"type":"CORPORATIONS"}'
```

Get company by ID:
```bash
curl -s http://localhost:8080/v1/companies/<id>
```

Update company:
```bash
curl -s -X PATCH http://localhost:8080/v1/companies/<id> \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/merge-patch+json" \
  -d '{"amount_of_employees":50}'
```

Delete company:
```bash
curl -s -X DELETE http://localhost:8080/v1/companies/<id> -H "Authorization: Bearer $TOKEN"
```

`type` is enum, allowed values: `CORPORATIONS`, `NON_PROFIT`, `COOPERATIVE` and
`SOLE_PROPRIETORSHIP`.

## Run the service on your machine

```bash
make deps   # PostgreSQL, Redpanda and the tools in Docker
make run    # the service, configured from .env
```

## Tests

```bash
make test               # unit tests
make test-integration   # against real PostgreSQL and Redpanda in Docker
make lint
make postman            # the Postman collection against a running `make start` stack
```

`make help` lists the other targets.
