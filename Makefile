PREFIX=/usr/local
PROGRAM=vastai-helper

.PHONY: build clean install

bin/$(PROGRAM): src/*.go src/plugins/api/*.go src/plugins/autoprune/*.go src/plugins/netattach/*.go
	go build -o bin/$(PROGRAM) src/*.go

build: bin/$(PROGRAM)

clean:
	@rm -rf ./bin

install: bin/$(PROGRAM)
	mkdir -p $(PREFIX)/bin
	cp bin/$(PROGRAM) $(PREFIX)/bin/
	cp systemd/$(PROGRAM).service /etc/systemd/system/
