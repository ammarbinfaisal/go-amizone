# CapSolver Integration - Quick Start

This project now uses [CapSolver](https://www.capsolver.com/) for CAPTCHA solving, replacing the previous thermoptic setup.

## Quick Setup

1. **Get CapSolver API Key**
   ```bash
   # Sign up at https://www.capsolver.com/
   # Get your API key from dashboard
   ```

2. **Configure Environment**
   ```bash
   cp .env.example .env
   # Edit .env and set:
   # CAPSOLVER_API_KEY=your-key-here
   # PROXY=http://user:pass@host:port  (optional)
   ```

3. **Run with Docker**
   ```bash
   docker-compose up -d
   ```

## Architecture

```
┌─────────────────┐      ┌──────────────────┐      ┌─────────────┐
│  Browser-Login  │◄─────┤  Amizone API     │◄─────┤   Nginx     │
│  (Playwright +  │      │  (Go gRPC/HTTP)  │      │  (Port 8080)│
│   CapSolver)    │      │                  │      └─────────────┘
│  (Port 8082)    │      │  (Port 8081)     │
└────────┬────────┘      └──────────────────┘
         │                        │
         │                        │
         └────────┬───────────────┘
                  │
                  ▼
         ┌────────────────┐
         │   CapSolver    │
         │  API Service   │
         └────────────────┘
```

## Two Usage Modes

### 1. Direct Go Client (Simple)
```go
client, _ := amizone.NewClientWithOptions(
    amizone.Credentials{Username: "user", Password: "pass"},
    amizone.WithCapSolver("capsolver-api-key"),
)
attendance, _ := client.GetAttendance()
```

### 2. Browser-Based Login (Robust)
```bash
curl -X POST http://localhost:8082/login \
  -H "Content-Type: application/json" \
  -d '{"username":"user","password":"pass"}'
```

## What Changed?

### ❌ Removed
- Thermoptic proxy (~500MB Docker image)
- Chrome CDP container
- ProxyRouter service
- Complex hook system
- Custom certificates

### ✅ Added
- CapSolver integration (Go + Python)
- Browser-login service (Playwright)
- Proxy support via environment variable
- Cleaner architecture

## Benefits

| Feature | Before (Thermoptic) | After (CapSolver) |
|---------|-------------------|-------------------|
| Docker Images | 4 services | 2 services |
| Setup Complexity | High | Low |
| Cost | Infrastructure | Pay-per-solve (~$0.001) |
| Maintenance | Complex hooks | Simple API calls |
| Proxy Support | Built-in | Via ENV var |
| Reliability | Browser automation | API + Browser |

## Proxy Support

Both CapSolver tasks and browser traffic support proxies:

```bash
# Set in .env
PROXY=http://user:pass@proxy.example.com:8080
```

When proxy is set:
- CapSolver uses `AntiTurnstileTask` (proxy mode)
- Browser traffic routes through proxy
- API requests use proxy

Without proxy:
- CapSolver uses `AntiTurnstileTaskProxyLess`
- Direct connections

## Costs

CapSolver pricing (approximate):
- Cloudflare Turnstile: $0.0005 - $0.002 per solve
- reCAPTCHA v2: $0.001 - $0.003 per solve

Typical usage:
- 100 logins/day = ~$0.10 - $0.30/day
- Much cheaper than running browser infrastructure 24/7

## Documentation

- 📘 [Complete Integration Guide](./CAPSOLVER_INTEGRATION.md)
- 🐳 [Browser-Login Service](./browser-login/README.md)
- 🔧 [Environment Variables](./.env.example)

## Testing

```bash
# Test browser-login service
curl http://localhost:8082/health

# Test login
curl -X POST http://localhost:8082/login \
  -H "Content-Type: application/json" \
  -d '{"username":"test","password":"test"}' | jq .

# Test amizone-api
curl http://localhost:8080/health
```

## Troubleshooting

### CapSolver Issues
```bash
# Check API key is set
echo $CAPSOLVER_API_KEY

# Check account balance at https://dashboard.capsolver.com/
```

### Browser Login Issues
```bash
# Check browser-login logs
docker-compose logs browser-login

# Check if Playwright is installed
docker-compose exec browser-login playwright --version
```

### Proxy Issues
```bash
# Test proxy connectivity
curl -x $PROXY https://api.capsolver.com/

# Check proxy format
echo $PROXY  # Should be: http://user:pass@host:port
```

## Migration Checklist

If migrating from thermoptic:

- [x] Remove thermoptic submodule
- [x] Update docker-compose.yml
- [x] Remove hooks directory
- [x] Get CapSolver API key
- [x] Set CAPSOLVER_API_KEY in .env
- [ ] Test login with new setup
- [ ] Update any CI/CD scripts
- [ ] Remove thermoptic-related documentation

## Support

- GitHub Issues: [go-amizone/issues](https://github.com/ditsuke/go-amizone/issues)
- CapSolver Docs: https://docs.capsolver.com/
- Playwright Docs: https://playwright.dev/python/
