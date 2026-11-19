# Common tasks. Each component also works on its own (see README).
.PHONY: up down logs test test-agent test-ingest test-ingest-integration test-api test-web e2e loadtest proto fmt

up:            ## Build and start the whole platform (UI on http://localhost:3000)
	docker compose up --build --detach --wait
down:
	docker compose down -v
logs:
	docker compose logs -f --tail 100

test: test-agent test-ingest test-api test-web  ## All unit tests

test-agent:
	$(MAKE) -C agent test asan
test-ingest:
	cd services/ingest && go vet ./... && go test -race ./...
test-ingest-integration:  ## Needs Postgres: TEST_DATABASE_URL=postgres://... make test-ingest-integration
	cd services/ingest && go test -tags=integration -count=1 ./internal/store/...
test-api:      ## Unit + integration (Testcontainers needs Docker)
	cd services/fleet-api && mvn -B verify
test-web:
	cd web && npm ci && npm run lint && npm run typecheck && npm test

e2e:           ## Against a running stack (make up)
	cd e2e && npm ci && npx playwright install chromium && npx playwright test

loadtest:      ## 60 s at 6,000 reports/s against the running stack; fails below 5,000/s persisted
	docker compose run --rm --no-deps --entrypoint /loadtest simulator \
		-target ingest:9090 -metrics http://ingest:8081/metrics -rate 6000 -duration 60s -min-rate 5000

proto:         ## Regenerate Go stubs (protoc + protoc-gen-go v1.36.11 + protoc-gen-go-grpc v1.5.1)
	protoc -I proto --go_out=services/ingest --go_opt=module=satfleet/ingest \
		--go-grpc_out=services/ingest --go-grpc_opt=module=satfleet/ingest \
		proto/fleet/telemetry/v1/telemetry.proto

fmt:
	cd services/ingest && gofmt -w .
	cd web && npx eslint --fix .
