tpb-backup: *.go
	go build -v -i

test: tpb-backup
	./tpb-backup

watch:
	find Makefile *.go | entr make test
