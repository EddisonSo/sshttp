.PHONY: all build clean dev client server install restart

# Build the complete binary with embedded frontend
all: build

build: client
	@echo "Copying frontend to server..."
	rm -rf server/cmd/sshttpd/static
	cp -r client/dist server/cmd/sshttpd/static
	@echo "Building server..."
	cd server && go build -o sshttpd ./cmd/sshttpd

# Build just the frontend
client:
	@echo "Building frontend..."
	cd client && npm run build

# Build server without embedding (uses external static dir)
server:
	cd server && go build -tags noembedded -o sshttpd ./cmd/sshttpd

# Development mode
dev:
	cd client && npm run dev &
	cd server && go run ./cmd/sshttpd

clean:
	rm -rf server/sshttpd server/cmd/sshttpd/static client/dist

install: build
	@sudo pkill -9 -x sshttpd 2>/dev/null || true
	@sleep 1
	sudo cp server/sshttpd /usr/local/bin/sshttpd
	@echo "Installed sshttpd to /usr/local/bin"

restart: install
	@echo "Restarting sshttp..."
	@if systemctl is-active --quiet sshttp 2>/dev/null; then \
		sudo systemctl restart sshttp; \
	else \
		pkill -9 -x sshttpd 2>/dev/null || true; \
		for i in 1 2 3 4 5; do \
			if ! pgrep -x sshttpd >/dev/null 2>&1; then break; fi; \
			sleep 1; \
		done; \
		if pgrep -x sshttpd >/dev/null 2>&1; then \
			echo "Error: failed to stop sshttpd"; exit 1; \
		fi; \
		nohup /usr/local/bin/sshttpd > /tmp/sshttpd.log 2>&1 & \
		sleep 1; \
		if ! pgrep -x sshttpd >/dev/null 2>&1; then \
			echo "Error: sshttpd failed to start. Check /tmp/sshttpd.log"; exit 1; \
		fi; \
	fi
	@echo "Restarted"
