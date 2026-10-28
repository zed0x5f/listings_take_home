# Single image: the Go API also serves the built React UI, so one container is one origin.

# 1. Build the frontend.
FROM node:22-slim AS web
WORKDIR /src/my-app
COPY my-app/package.json my-app/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY my-app/ ./
RUN npm run build

# 2. Build the backend. modernc.org/sqlite is pure Go, so no cgo is needed.
FROM golang:1.27-alpine AS api
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# 3. Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api /out/server ./server
COPY --from=web /src/my-app/dist ./public
COPY sample_listings.json ./
# In-memory DB is re-seeded on every start, so no disk or volume is required.
# The listen port comes from $PORT (set by Render), falling back to 8080.
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/app/server", "-db", ":memory:", "-seed", "sample_listings.json", "-static", "public"]
