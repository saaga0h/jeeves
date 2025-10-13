# E2E Testing Framework - Setup Checklist

Use this checklist to verify your environment is ready for E2E testing.

## Prerequisites

### 1. Docker
- [ ] Docker installed: `docker --version`
- [ ] Docker daemon running: `docker ps`

### 2. Docker Compose
- [ ] Docker Compose installed: `docker-compose --version`
- [ ] Can run compose: `docker-compose ps`

### 3. Go
- [ ] Go 1.21+ installed: `go version`
- [ ] Go modules enabled: `go env GO111MODULE` (should show "on" or empty)

### 4. Ollama
- [ ] Ollama installed: `ollama --version`
- [ ] Ollama running: `curl http://localhost:11434/api/tags`
- [ ] Model downloaded: `ollama list | grep mixtral:8x7b`
  - If not: `ollama pull mixtral:8x7b`

### 5. Make
- [ ] Make installed: `make --version`

## Verification

Run the verification command:
```bash
cd e2e
make verify
```

You should see:
```
✅ Docker is installed
✅ Docker Compose is installed
✅ Ollama is installed
✅ Ollama is running
✅ All prerequisites satisfied
```

## First-Time Setup

### 1. Build Agents
```bash
cd jeeves
make build
```

Expected output:
```
Building collector-agent for darwin/arm64...
Building illuminance-agent for darwin/arm64...
Building light-agent for darwin/arm64...
Building occupancy-agent for darwin/arm64...
```

### 2. Build Test Infrastructure
```bash
cd e2e
make build
```

Expected output:
```
Building observer...
Building test-runner...
```

### 3. Run Your First Test
```bash
make test SCENARIO=hallway_passthrough
```

This will:
1. Start all services
2. Run the test
3. Display results
4. Clean up

Expected duration: ~3 minutes

## Troubleshooting

### "Cannot connect to the Docker daemon"
```bash
# macOS
open -a Docker

# Linux
sudo systemctl start docker
```

### "ollama: command not found"
```bash
# macOS
brew install ollama

# Linux
curl https://ollama.ai/install.sh | sh
```

### "mixtral:8x7b model not found"
```bash
ollama pull mixtral:8x7b
```

This will download ~4.7GB. Wait for completion before running tests.

### "failed to build" or "module not found"
```bash
# From project root
go mod tidy
go mod download
```

### Port Already in Use
If ports 1883 (MQTT) or 6379 (Redis) are in use:

```bash
# Check what's using the ports
lsof -i :1883
lsof -i :6379

# Stop conflicting services or change ports in docker-compose.test.yml
```

## Post-Setup Verification

### Run All Tests
```bash
make test-all
```

All three scenarios should pass:
- ✓ hallway_passthrough
- ✓ study_working
- ✓ bedroom_morning

### Check Output Files
```bash
ls test-output/timelines/
ls test-output/captures/
ls test-output/summaries/
```

You should see files for all three scenarios.

### View a Timeline
```bash
make view-timeline SCENARIO=hallway_passthrough
```

You should see a formatted timeline report.

## Common First-Run Issues

### Test Hangs at "Waiting for services to be ready"
**Cause**: Services not starting properly

**Solution**:
```bash
make logs
```

Look for errors in startup logs.

### "No messages found for topic"
**Cause**: Agents not publishing or timing issues

**Solution**:
1. Check agent logs: `make logs SERVICE=collector`
2. Verify MQTT broker: `make logs SERVICE=mosquitto`
3. Increase wait times in scenario YAML

### Flaky Test Results
**Cause**: LLM variability or timing issues

**Solution**:
1. Run test 3 times to verify consistency
2. Use flexible matchers: `">0.6"` instead of exact values
3. Add buffer time to expectations

## Success Criteria

You're ready to use the framework when:
- ✅ `make verify` passes all checks
- ✅ `make build` completes without errors
- ✅ At least one test scenario passes
- ✅ Test output files are generated
- ✅ Timeline reports are readable

## Next Steps

Once setup is complete:

1. **Read the documentation**:
   - [README.md](README.md) - Framework overview
   - [QUICKSTART.md](QUICKSTART.md) - Common operations
   - [test-scenarios/README.md](../test-scenarios/README.md) - Writing scenarios

2. **Explore existing scenarios**:
   ```bash
   cat ../test-scenarios/hallway_passthrough.yaml
   cat ../test-scenarios/study_working.yaml
   cat ../test-scenarios/bedroom_morning.yaml
   ```

3. **Write your first scenario**:
   - Copy an existing scenario
   - Modify events and expectations
   - Run the test
   - Iterate

## Getting Help

If you're stuck:

1. Check [e2e/README.md](README.md) for detailed documentation
2. Review existing test scenarios for examples
3. Run `make logs` to see what's happening
4. Check Docker container status: `make status`
5. Verify prerequisites: `make verify`

## Clean Slate

If you need to start fresh:

```bash
# Stop everything and remove volumes
make clean

# Remove all test output
rm -rf test-output/*

# Rebuild from scratch
make build
```

Then start from step 3 of First-Time Setup.
