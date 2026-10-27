.PHONY: run verify test build seed-large clean

run:
	./scripts/run.sh

verify:
	./scripts/verify.sh

test:
	GIN_MODE=release go test ./...

build:
	go build ./...
	cd web && npm run build

seed-large:
	./scripts/seed-large.sh

clean:
	rm -rf data web/dist
