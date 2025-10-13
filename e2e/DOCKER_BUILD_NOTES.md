# Docker Build Notes

## Go Version Management

The project uses dependencies that require Go 1.24+, but Docker images start with Go 1.23. We use `GOTOOLCHAIN=auto` to allow Go to automatically download and use the required toolchain version during the build process.

This is set in all Dockerfiles:
- `Dockerfile` (agents)
- `e2e/Dockerfile.observer`
- `e2e/Dockerfile.test-runner`

## Building Images

```bash
# Build all services
docker-compose -f e2e/docker-compose.test.yml build

# Build specific service
docker-compose -f e2e/docker-compose.test.yml build collector
docker-compose -f e2e/docker-compose.test.yml build observer
docker-compose -f e2e/docker-compose.test.yml build test-runner
```

## First Build

The first build will take longer because:
1. Go toolchain may be downloaded (if newer version needed)
2. All dependencies are downloaded
3. All agents are compiled

Subsequent builds are cached and much faster.

## Troubleshooting

### "go mod requires go >= X.X.X"
This should be handled automatically by `GOTOOLCHAIN=auto`. If you still see this error, verify the Dockerfile contains:
```dockerfile
ENV GOTOOLCHAIN=auto
```

### "module not found" errors
Ensure `go.mod` and `go.sum` are up to date in the project root:
```bash
go mod tidy
go mod download
```

### Build hangs or times out
Increase Docker resource limits in Docker Desktop preferences:
- CPU: 4+ cores
- Memory: 4+ GB
- Disk space: ensure at least 10GB free
