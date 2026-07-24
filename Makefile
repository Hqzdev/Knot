.PHONY: fmt-check proto-check legacy-check go-check web-check check compose-config up smoke e2e down logs

fmt-check:
	test -z "$$(gofmt -l $$(rg --files services proto -g '*.go'))"

proto-check:
	buf lint proto

legacy-check:
	./scripts/check_plaintext_product.sh

go-check:
	go test -race ./...
	go vet ./...

web-check:
	npm --prefix clients/web run check

check: fmt-check proto-check legacy-check go-check web-check

compose-config:
	docker compose config --quiet

up:
	docker compose up --build -d

smoke:
	./scripts/smoke_stack.sh

e2e:
	npm --prefix clients/web run test:e2e

down:
	docker compose down

logs:
	docker compose logs -f --tail=200
