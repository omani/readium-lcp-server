go-tidy:
	go mod tidy -go=1.16 && go mod tidy -go=1.17

build-lcpserver:
	go build -o builds/lcpserver lcpserver/lcpserver.go

build-lsdserver:
	go build -o builds/lsdserver lsdserver/lsdserver.go

build-lcpencrypt:
	go build -o builds/lcpencrypt lcpencrypt/lcpencrypt.go

.PHONY: tidy build-lcpserver build-lsdserver build-lcpencrypt