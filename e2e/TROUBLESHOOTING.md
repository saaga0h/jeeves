# E2E Testing Framework - Troubleshooting Guide

## Common Issues and Solutions

### Build Issues

#### "go: updates to go.mod needed; to update it: go mod tidy"
**Solution:**
```bash
cd /path-to-projects/jeeves
go mod tidy
```

Then try running the test again.

#### "open docker-compose.test.yml: no such file or directory"
**Cause:** Script is being run from wrong directory

**Solution:** Always run from the `e2e` directory:
```bash
cd e2e
./run-test.sh hallway_passthrough
```

Or use the Makefile which handles paths correctly:
```bash
cd e2e
make test SCENARIO=hallway_passthrough
```

#### "go mod requires go >= 1.24.0"
This should be handled automatically by `GOTOOLCHAIN=auto` in the Dockerfiles.

**If you still see this:**
1. Check that Dockerfiles contain `ENV GOTOOLCHAIN=auto`
2. Rebuild without cache: `docker-compose -f docker-compose.test.yml build --no-cache`

### Docker Issues

#### "Cannot connect to the Docker daemon"
**Solution:**
```bash
# macOS
open -a Docker

# Linux
sudo systemctl start docker
```

#### "Port already in use" (1883 or 6379)
**Check what's using the ports:**
```bash
lsof -i :1883
lsof -i :6379
```

**Solutions:**
1. Stop conflicting services
2. Or change ports in `docker-compose.test.yml`

#### Containers won't start or crash immediately
**Check logs:**
```bash
cd e2e
make logs SERVICE=collector
make logs SERVICE=occupancy-agent
```

**Common causes:**
- Environment variables not set correctly
- Dependencies not available (MQTT, Redis)
- Binary built for wrong architecture

### Test Execution Issues

#### "No messages found for topic"
**Causes:**
- Agents not publishing to expected topics
- Timing too tight (check before agent processes)
- Wrong topic format in expectations

**Debug:**
```bash
# Check MQTT capture
cat e2e/test-output/captures/<scenario>.json | jq '.[] | .topic'

# Watch real-time MQTT traffic
cd e2e
make observer
```

#### "LLM connection failed" or occupancy agent errors
**Check Ollama:**
```bash
curl http://localhost:11434/api/tags
```

**If not running:**
```bash
# Start Ollama
ollama serve

# In another terminal, ensure model is available
ollama pull mixtral:8x7b
```

#### Test hangs at "Waiting for services to be ready"
**Check service health:**
```bash
cd e2e
docker-compose -f docker-compose.test.yml ps
```

**Check individual services:**
```bash
make logs SERVICE=mosquitto
make logs SERVICE=redis
make logs SERVICE=collector
```

### Scenario Issues

#### Flaky test results (passes sometimes, fails others)
**Cause:** LLM variability or timing sensitivity

**Solutions:**
1. Use flexible matchers: `">0.5"` instead of exact values
2. Use regex patterns: `"~occupied|likely~"`
3. Increase wait times before expectations
4. Run test multiple times to verify consistency

#### Expectation always fails
**Debug steps:**
1. Check actual MQTT messages:
   ```bash
   cat e2e/test-output/captures/<scenario>.json | jq
   ```

2. Find the specific topic:
   ```bash
   cat e2e/test-output/captures/<scenario>.json | \
     jq '.[] | select(.topic == "occupancy/status/hallway")'
   ```

3. Compare actual vs expected payload

4. Adjust expectations or timing in YAML

### Performance Issues

#### Tests run very slowly
**Possible causes:**
1. First build (downloads toolchain and dependencies) - normal
2. Low Docker resources
3. Ollama using CPU instead of GPU

**Solutions:**
- Increase Docker resources (CPU/Memory) in Docker Desktop
- For Ollama on Apple Silicon, ensure metal acceleration is working
- Subsequent test runs will be much faster (cached images)

#### Build takes forever
**Increase Docker resources:**
- Docker Desktop → Settings → Resources
- Recommended: 4+ CPU cores, 4+ GB RAM

### Path Issues

#### Scripts can't find files
**Always run from the e2e directory:**
```bash
cd e2e
./run-test.sh hallway_passthrough
```

Or use absolute paths:
```bash
/path-to-projects/jeeves/e2e/run-test.sh hallway_passthrough
```

Or use the Makefile (handles paths automatically):
```bash
cd e2e
make test SCENARIO=hallway_passthrough
```

## Clean Slate

If everything is broken and you want to start fresh:

```bash
cd e2e

# Stop and remove everything
make clean-all

# Remove all test output
rm -rf test-output/*

# Rebuild from scratch
make build

# Try a simple test
make test SCENARIO=hallway_passthrough
```

## Getting More Information

### View all logs
```bash
cd e2e
make logs
```

### View specific service logs
```bash
make logs SERVICE=occupancy-agent
make logs SERVICE=collector
make logs SERVICE=mosquitto
```

### Check service status
```bash
make status
```

### Verify prerequisites
```bash
make verify
```

### Inspect MQTT traffic in real-time
```bash
# Start observer
make observer

# In another terminal, manually publish test events
make publish TOPIC=sensor/motion/test VALUE='{"sensorType":"motion","value":true}'
```

### Check Redis state
```bash
make shell-redis

# In Redis CLI:
KEYS *
HGETALL sensor:motion:hallway-sensor-1
```

## Still Stuck?

1. Check [README.md](README.md) for complete documentation
2. Check [QUICKSTART.md](QUICKSTART.md) for common operations
3. Review existing test scenarios for examples
4. Check [SETUP_CHECKLIST.md](SETUP_CHECKLIST.md) for prerequisites

## Reporting Issues

When reporting issues, include:
1. Complete error message
2. Output from `make verify`
3. Output from `make status`
4. Relevant logs from `make logs`
5. Steps to reproduce
