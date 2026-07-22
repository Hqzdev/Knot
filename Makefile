.PHONY: fmt-check crypto-check go-check web-check check apple-framework apple-ios apple-macos compose-config up smoke e2e down logs

fmt-check:
	test -z "$$(gofmt -l $$(rg --files services proto -g '*.go'))"
	cargo fmt --check

crypto-check:
	cargo clippy --all-targets --all-features -- -D warnings
	cargo test --all-features
	cargo check --target wasm32-unknown-unknown -p knot-crypto-core

go-check:
	go test -race ./...
	go vet ./...

web-check:
	npm --prefix clients/web run check

check: fmt-check crypto-check go-check web-check

apple-framework:
	./scripts/build_ios_xcframework.sh

apple-ios: apple-framework
	xcodebuild -project clients/apple/Knot/Knot.xcodeproj -scheme Knot -configuration Debug -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build

apple-macos: apple-framework
	xcodebuild -project clients/apple/Knot/Knot.xcodeproj -scheme Knot -configuration Debug -destination 'generic/platform=macOS' CODE_SIGNING_ALLOWED=NO build

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
