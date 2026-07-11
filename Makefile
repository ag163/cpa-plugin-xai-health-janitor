# Build Linux plugin for CPA
# Requires: Go 1.26+, gcc (for cgo)

.PHONY: linux windows clean

linux:
	cd go && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o ../xai-health-janitor.so .
	rm -f xai-health-janitor.h go/xai-health-janitor.h

windows:
	cd go && CGO_ENABLED=1 go build -buildmode=c-shared -o ../xai-health-janitor.dll .
	rm -f xai-health-janitor.h go/xai-health-janitor.h

clean:
	rm -f xai-health-janitor.so xai-health-janitor.dll xai-health-janitor.h go/*.h
