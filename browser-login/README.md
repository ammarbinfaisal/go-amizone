# Browser-Based Login Service

This service provides browser-based authentication for Amizone with automatic CAPTCHA solving using CapSolver.

## Features

- **Automated Login**: Uses Playwright to handle browser-based authentication
- **CAPTCHA Solving**: Integrates with CapSolver to automatically solve Cloudflare Turnstile challenges
- **Session Management**: Returns session cookies that can be used with curl_cffi or other HTTP clients
- **Proxy Support**: Configurable HTTP proxy support for routing traffic
- **FastAPI**: RESTful API for easy integration

## API Endpoints

### POST /login

Performs browser-based login and returns session cookies.

**Request:**
```json
{
  "username": "your-username",
  "password": "your-password"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Login successful",
  "cookies": {
    "ASP.NET_SessionId": "...",
    ".ASPXAUTH": "...",
    ...
  },
  "session_id": "..."
}
```

### GET /health

Health check endpoint.

## Configuration

Environment variables:

- `PORT` - Service port (default: 8082)
- `CAPSOLVER_API_KEY` - CapSolver API key (required)
- `PROXY` - HTTP proxy URL (optional, format: `http://user:pass@host:port`)
- `BROWSER_HEADLESS` - Run browser in headless mode (default: true)

## Local Development

```bash
# Install dependencies
pip install -r requirements.txt

# Install Playwright browsers
playwright install chromium

# Set environment variables
export CAPSOLVER_API_KEY=your-key-here
export PROXY=http://your-proxy:port  # optional

# Run the service
python main.py
```

## Docker

The service is designed to run in Docker with the main application.

```bash
# Build
docker build -t browser-login .

# Run
docker run -p 8082:8082 \
  -e CAPSOLVER_API_KEY=your-key \
  -e PROXY=http://proxy:port \
  browser-login
```

## Testing with curl

```bash
# Login and get cookies
curl -X POST http://localhost:8082/login \
  -H "Content-Type: application/json" \
  -d '{"username":"your-user","password":"your-pass"}' | jq .

# Use cookies with curl_cffi (in Go or Python)
# The returned cookies can be used for subsequent requests
```

## Integration Flow

1. Client sends credentials to `/login` endpoint
2. Service launches browser with Playwright
3. Navigates to Amizone login page
4. Detects Cloudflare Turnstile challenge
5. Solves CAPTCHA using CapSolver API
6. Injects solution token into the page
7. Fills credentials and submits form
8. Returns session cookies on success

## Notes

- The service uses Playwright (Chromium) for browser automation
- CapSolver handles Cloudflare Turnstile challenges
- Session cookies can be used with curl_cffi or similar libraries that support cookie jars
- The browser runs in headless mode by default for efficiency
