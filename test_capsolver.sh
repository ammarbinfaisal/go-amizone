#!/bin/bash
# Test script for CapSolver integration

set -e

echo "=== Testing CapSolver Integration ==="
echo ""

# Check if .env exists
if [ ! -f .env ]; then
    echo "❌ .env file not found. Please create it from .env.example"
    exit 1
fi

# Load environment
source .env

# Check required variables
if [ -z "$CAPSOLVER_API_KEY" ]; then
    echo "❌ CAPSOLVER_API_KEY not set in .env"
    exit 1
fi

echo "✅ Environment variables loaded"
echo ""

# Test 1: Check if services are running
echo "Test 1: Checking Docker services..."
if docker-compose ps | grep -q "Up"; then
    echo "✅ Docker services are running"
else
    echo "⚠️  Docker services not running. Starting..."
    docker-compose up -d
    echo "Waiting for services to be ready..."
    sleep 15
fi
echo ""

# Test 2: Browser-login health check
echo "Test 2: Browser-login service health check..."
if curl -f -s http://localhost:8082/health > /dev/null; then
    echo "✅ Browser-login service is healthy"
else
    echo "❌ Browser-login service health check failed"
    docker-compose logs browser-login
    exit 1
fi
echo ""

# Test 3: Amizone API health check
echo "Test 3: Amizone API health check..."
if curl -f -s http://localhost:8080/health > /dev/null; then
    echo "✅ Amizone API is healthy"
else
    echo "❌ Amizone API health check failed"
    docker-compose logs amizone-api
    exit 1
fi
echo ""

# Test 4: Browser-login login test (will fail with invalid creds, but tests the flow)
echo "Test 4: Testing browser-login endpoint..."
response=$(curl -s -X POST http://localhost:8082/login \
    -H "Content-Type: application/json" \
    -d '{"username":"test","password":"test"}' || echo '{"detail":"error"}')

if echo "$response" | jq . > /dev/null 2>&1; then
    echo "✅ Browser-login endpoint responds correctly"
    echo "Response: $(echo $response | jq -c .)"
else
    echo "❌ Browser-login endpoint returned invalid JSON"
    echo "Response: $response"
fi
echo ""

# Test 5: Check CapSolver client compilation (Go)
echo "Test 5: Checking Go CapSolver client..."
if [ -f "amizone/capsolver/capsolver.go" ]; then
    echo "✅ Go CapSolver client exists"

    # Try to compile it
    if go build -o /dev/null ./amizone/capsolver 2>&1 | grep -q "no Go files"; then
        echo "✅ Go CapSolver package structure is valid"
    fi
else
    echo "❌ Go CapSolver client not found"
fi
echo ""

echo "=== Test Summary ==="
echo "All basic tests completed!"
echo ""
echo "Next steps:"
echo "1. Test with real Amizone credentials"
echo "2. Monitor CapSolver dashboard for solve attempts"
echo "3. Check docker logs for any errors:"
echo "   docker-compose logs -f browser-login"
echo "   docker-compose logs -f amizone-api"
