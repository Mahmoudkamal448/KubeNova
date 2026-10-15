.PHONY: all backend test frontend dev dist clean mcp

all: backend frontend

backend:
	cd backend && go build -trimpath -o bin/kubenova-server ./cmd/kubenova-server

test:
	cd backend && go vet ./... && go test ./...
	cd frontend && npm run typecheck

frontend:
	cd frontend && npm ci && npm run build

# Run the backend on a fixed port and the UI in a browser (no Electron needed):
#   http://localhost:5173/?backend=http://127.0.0.1:7443&token=dev
dev: backend
	cd backend && ./bin/kubenova-server --listen 127.0.0.1:7443 --token dev & \
	cd frontend && npx vite

# Desktop installers for the current OS (Windows: NSIS/portable, macOS: dmg/zip, Linux: AppImage/deb/rpm)
dist:
	cd frontend && npm ci && npm run dist

# FIPS 140-3 backend build (Go native FIPS module)
fips:
	cd backend && GOFIPS140=v1.0.0 go build -trimpath -o bin/kubenova-server ./cmd/kubenova-server

mcp: backend
	./backend/bin/kubenova-server --mcp

clean:
	rm -rf backend/bin frontend/dist frontend/dist-electron frontend/release frontend/resources/bin/*
