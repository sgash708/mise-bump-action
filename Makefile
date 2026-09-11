.PHONY: build test lint fmt release

build:
	go build -o bin/mise-bump-action ./cmd/mise-bump-action

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .
	goimports -l -w .

release:
	@test -n "$(VERSION)" || (echo "VERSION is required, e.g. make release VERSION=v0.1.0" && exit 1)
	sed -i.bak -E 's#(gh release download )v[0-9]+\.[0-9]+\.[0-9]+( \\)#\1$(VERSION)\2#' action.yml
	rm -f action.yml.bak
	git add action.yml
	git commit -m "chore: release $(VERSION)"
	git tag $(VERSION)
	@echo "Now run: git push && git push origin $(VERSION)"
