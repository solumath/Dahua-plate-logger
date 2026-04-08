BIN    := plate_logger
DB_DIR := plates
OUT    := exports

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN) .

run:
	go run .

test:
	go test ./internal/...

export-today:
	go run ./cmd/export/ -range today  -db $(DB_DIR) -out $(OUT)

export-week:
	go run ./cmd/export/ -range week   -db $(DB_DIR) -out $(OUT)

export-month:
	go run ./cmd/export/ -range month  -db $(DB_DIR) -out $(OUT)

export-year:
	go run ./cmd/export/ -range year   -db $(DB_DIR) -out $(OUT)

export-all:
	go run ./cmd/export/ -range all    -db $(DB_DIR) -out $(OUT)

install-hooks:
	git config core.hooksPath .githooks

clean:
	rm -f $(BIN)

.PHONY: build run test export-today export-week export-month export-year export-all install-hooks clean
