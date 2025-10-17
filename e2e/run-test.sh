#!/bin/bash
set -e

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

SCENARIO=$1

if [ -z "$SCENARIO" ]; then
    echo "Usage: ./run-test.sh <scenario-name>"
    echo "Available scenarios:"
    ls -1 ../test-scenarios/*.yaml 2>/dev/null | xargs -n1 basename -s .yaml || echo "No scenarios found"
    exit 1
fi

# Ensure clean state
echo "Cleaning up previous test environment..."
docker-compose -f docker-compose.test.yml down -v 2>/dev/null || true

# Build agents
echo "Building agents..."
(cd .. && make build)

# Build test images
echo "Building test infrastructure..."
docker-compose -f docker-compose.test.yml build

# Start infrastructure and agents
echo "Starting test environment..."
docker-compose -f docker-compose.test.yml up -d mosquitto redis postgres collector illuminance-agent light-agent occupancy-agent behavior-agent observer-agent

# Wait for services to be ready
echo "Waiting for services to be ready..."
sleep 10

# Run test
echo "Running scenario: $SCENARIO"
docker-compose -f docker-compose.test.yml run --rm test-runner \
  --scenario "/scenarios/${SCENARIO}.yaml" \
  --output-dir /output \
  --verbose

# Capture exit code
EXIT_CODE=$?

# Show results
echo ""
echo "========================================="
echo "Test Results"
echo "========================================="
if [ -f "test-output/timelines/${SCENARIO}.txt" ]; then
    cat "test-output/timelines/${SCENARIO}.txt"
else
    echo "Timeline not found: test-output/timelines/${SCENARIO}.txt"
fi

# Cleanup
#echo ""
#echo "Cleaning up..."
#docker-compose -f docker-compose.test.yml down

exit $EXIT_CODE
