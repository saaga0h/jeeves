#!/bin/bash

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

FAILED=0
PASSED=0

echo "========================================="
echo "Running All E2E Test Scenarios"
echo "========================================="
echo ""

for scenario in ../test-scenarios/*.yaml; do
    if [ ! -f "$scenario" ]; then
        echo "No test scenarios found in ../test-scenarios/"
        exit 1
    fi

    name=$(basename "$scenario" .yaml)
    echo "========================================="
    echo "Running: $name"
    echo "========================================="

    if ./run-test.sh "$name"; then
        echo "✓ $name PASSED"
        PASSED=$((PASSED + 1))
    else
        echo "✗ $name FAILED"
        FAILED=$((FAILED + 1))
    fi
    echo ""
done

echo "========================================="
echo "Test Summary"
echo "========================================="
echo "Passed: $PASSED"
echo "Failed: $FAILED"
echo ""

if [ $FAILED -eq 0 ]; then
    echo "✓ All tests passed!"
    exit 0
else
    echo "✗ $FAILED test(s) failed"
    exit 1
fi
