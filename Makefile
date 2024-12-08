all:
	go build ./cmd/vhost/
	go build ./cmd/vrouter/

clean:
	rm -f $(wildcard vrouter*)
	rm -f $(wildcard vhost*)