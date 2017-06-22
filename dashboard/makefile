help:
	@echo "You can perform the following:"
	@echo ""
	@echo "  check         Format, lint, vet, and test Go code"
	@echo "  generate      Run \`go generate\`"
	@echo "  local         Build for local development OS"
	@echo "  arm           Build for ARM architecture"

# Format, lint, vet, and test the Go code
check:
	@echo 'Formatting, linting, vetting, and testing Go code'
	go fmt ./...
	golint ./...
	go vet ./...
	go test ./...

#  Run `go generate`
generate:
	go generate

#  Compile the project to run locally on your machine
local: generate
	go build

#  Compile the project to run on the targeted ARM processor
arm: generate
	env CC="/usr/local/bin/arm-none-eabi-gcc" GOARCH=arm GOARM=7 CGO_ENABLED=1 go build -ldflags='-extld=$CC'
