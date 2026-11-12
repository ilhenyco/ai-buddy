TEST_DATABASE_URL ?= postgres://homych:homych@localhost:55432/postgres?sslmode=disable
export TEST_DATABASE_URL

.PHONY: db run test cover lint

db:            ## start only PostgreSQL from the root docker-compose.yml
	docker compose -f ../docker-compose.yml up -d db

run: db        ## run the API locally on :8080
	DATABASE_URL=postgres://homych:homych@localhost:55432/homych?sslmode=disable go run ./cmd/api

test:          ## all tests (needs PostgreSQL: make db)
	go test -race -count=1 ./...

cover:         ## coverage of application code, must stay > 80%
	go test -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"
