"""
Browser-based login service for Amizone
Handles authentication with CAPTCHA solving via CapSolver
"""
import asyncio
import json
import logging
import os
from contextlib import asynccontextmanager
from typing import Optional

import httpx
from fastapi import FastAPI, HTTPException
from playwright.async_api import Browser, async_playwright
from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class Settings(BaseSettings):
    """Application settings"""
    port: int = Field(default=8082, env='PORT')
    proxy: Optional[str] = Field(default=None, env='PROXY')
    capsolver_api_key: str = Field(..., env='CAPSOLVER_API_KEY')
    amizone_url: str = 'https://s.amizone.net/'
    browser_headless: bool = Field(default=True, env='BROWSER_HEADLESS')

    class Config:
        env_file = '.env'
        env_file_encoding = 'utf-8'


settings = Settings()


class LoginRequest(BaseModel):
    """Login request payload"""
    username: str
    password: str


class LoginResponse(BaseModel):
    """Login response payload"""
    success: bool
    message: str
    cookies: Optional[dict] = None
    session_id: Optional[str] = None


class CapSolverClient:
    """Client for CapSolver API"""

    def __init__(self, api_key: str, proxy: Optional[str] = None):
        self.api_key = api_key
        self.base_url = "https://api.capsolver.com"
        self.proxy = proxy

    def _parse_proxy(self) -> Optional[dict]:
        """Parse proxy URL into CapSolver format"""
        if not self.proxy:
            return None

        # Parse proxy URL: http://user:pass@host:port or http://host:port
        from urllib.parse import urlparse
        parsed = urlparse(self.proxy)

        proxy_info = {
            "proxyType": parsed.scheme,  # http, https, socks5
            "proxyAddress": f"{parsed.hostname}:{parsed.port}"
        }

        if parsed.username:
            proxy_info["proxyLogin"] = parsed.username
        if parsed.password:
            proxy_info["proxyPassword"] = parsed.password

        return proxy_info

    async def solve_turnstile(self, website_url: str, website_key: str) -> str:
        """Solve Cloudflare Turnstile challenge"""
        proxy_info = self._parse_proxy()
        task_type = "AntiTurnstileTask" if proxy_info else "AntiTurnstileTaskProxyLess"

        task_data = {
            "type": task_type,
            "websiteURL": website_url,
            "websiteKey": website_key,
        }

        if proxy_info:
            task_data["proxy"] = proxy_info
            logger.info(f"Using proxy for Turnstile: {proxy_info['proxyAddress']}")

        async with httpx.AsyncClient(timeout=120.0) as client:
            # Create task
            create_response = await client.post(
                f"{self.base_url}/createTask",
                json={
                    "clientKey": self.api_key,
                    "task": task_data
                }
            )
            create_data = create_response.json()

            if create_data.get('errorId') != 0:
                raise Exception(f"CapSolver error: {create_data.get('errorDescription')}")

            task_id = create_data['taskId']
            logger.info(f"Created CapSolver task: {task_id}")

            # Poll for result
            for _ in range(60):  # 60 attempts * 2 seconds = 2 minutes max
                await asyncio.sleep(2)

                result_response = await client.post(
                    f"{self.base_url}/getTaskResult",
                    json={
                        "clientKey": self.api_key,
                        "taskId": task_id
                    }
                )
                result_data = result_response.json()

                if result_data.get('errorId') != 0:
                    raise Exception(f"CapSolver error: {result_data.get('errorDescription')}")

                if result_data.get('status') == 'ready':
                    token = result_data['solution']['token']
                    logger.info(f"Got CapSolver token for task {task_id}")
                    return token

            raise Exception("Timeout waiting for CapSolver solution")

    async def solve_recaptcha_v2(self, website_url: str, website_key: str) -> str:
        """Solve reCAPTCHA v2 challenge"""
        proxy_info = self._parse_proxy()
        task_type = "ReCaptchaV2Task" if proxy_info else "ReCaptchaV2TaskProxyLess"

        task_data = {
            "type": task_type,
            "websiteURL": website_url,
            "websiteKey": website_key,
        }

        if proxy_info:
            task_data["proxy"] = proxy_info
            logger.info(f"Using proxy for reCAPTCHA: {proxy_info['proxyAddress']}")

        async with httpx.AsyncClient(timeout=120.0) as client:
            # Create task
            create_response = await client.post(
                f"{self.base_url}/createTask",
                json={
                    "clientKey": self.api_key,
                    "task": task_data
                }
            )
            create_data = create_response.json()

            if create_data.get('errorId') != 0:
                raise Exception(f"CapSolver error: {create_data.get('errorDescription')}")

            task_id = create_data['taskId']
            logger.info(f"Created CapSolver task for reCAPTCHA: {task_id}")

            # Poll for result
            for _ in range(60):  # 60 attempts * 2 seconds = 2 minutes max
                await asyncio.sleep(2)

                result_response = await client.post(
                    f"{self.base_url}/getTaskResult",
                    json={
                        "clientKey": self.api_key,
                        "taskId": task_id
                    }
                )
                result_data = result_response.json()

                if result_data.get('errorId') != 0:
                    raise Exception(f"CapSolver error: {result_data.get('errorDescription')}")

                if result_data.get('status') == 'ready':
                    token = result_data['solution']['gRecaptchaResponse']
                    logger.info(f"Got CapSolver reCAPTCHA token for task {task_id}")
                    return token

            raise Exception("Timeout waiting for CapSolver reCAPTCHA solution")


class BrowserLoginService:
    """Service for browser-based login"""

    def __init__(self):
        self.browser: Optional[Browser] = None
        self.playwright = None
        self.capsolver = CapSolverClient(settings.capsolver_api_key, settings.proxy)

    async def start(self):
        """Start the browser"""
        logger.info("Starting browser...")
        self.playwright = await async_playwright().start()

        launch_options = {
            'headless': settings.browser_headless,
            'args': [
                '--disable-blink-features=AutomationControlled',
                '--disable-dev-shm-usage',
                '--no-sandbox',
            ]
        }

        if settings.proxy:
            launch_options['proxy'] = {'server': settings.proxy}
            logger.info(f"Using proxy: {settings.proxy}")

        self.browser = await self.playwright.chromium.launch(**launch_options)
        logger.info("Browser started successfully")

    async def stop(self):
        """Stop the browser"""
        if self.browser:
            await self.browser.close()
        if self.playwright:
            await self.playwright.stop()
        logger.info("Browser stopped")

    async def login(self, username: str, password: str) -> LoginResponse:
        """
        Perform login with CAPTCHA solving
        Returns session cookies on success
        """
        if not self.browser:
            raise Exception("Browser not initialized")

        context = await self.browser.new_context(
            viewport={'width': 1920, 'height': 1080},
            user_agent='Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
        )

        try:
            page = await context.new_page()
            logger.info(f"Navigating to {settings.amizone_url}")

            # Navigate to login page
            await page.goto(settings.amizone_url, wait_until='domcontentloaded', timeout=60000)
            await asyncio.sleep(3)  # Wait for any initial loads

            # Check for Cloudflare Turnstile (check both iframe and the Capthcadiv container)
            turnstile_element = await page.query_selector('iframe[src*="challenges.cloudflare.com"]')
            captcha_div = await page.query_selector('#Capthcadiv')
            recaptcha_token_field = await page.query_selector('#RecaptchaToken')

            # Amizone uses Turnstile if RecaptchaToken field exists or Capthcadiv exists
            if turnstile_element or captcha_div or recaptcha_token_field:
                logger.info("Cloudflare Turnstile detected, solving with CapSolver...")

                # Extract turnstile sitekey from the page
                # Amizone uses turnstile.render() in a script, so we need to extract from there
                sitekey = await page.evaluate("""
                    () => {
                        // First try data-sitekey attribute
                        const turnstile = document.querySelector('[data-sitekey]');
                        if (turnstile) return turnstile.getAttribute('data-sitekey');

                        // Then try to find in script content (Amizone's approach)
                        const scripts = document.querySelectorAll('script');
                        for (const script of scripts) {
                            const text = script.textContent || '';
                            const match = text.match(/sitekey:\s*["']([^"']+)["']/);
                            if (match) return match[1];
                        }
                        return null;
                    }
                """)

                if not sitekey:
                    logger.warning("Could not find sitekey, using default Amizone sitekey")
                    sitekey = "0x4AAAAAAAwm6_gqjJdfOuzq"  # Current Amizone sitekey

                logger.info(f"Using Turnstile sitekey: {sitekey}")

                # Solve with CapSolver
                token = await self.capsolver.solve_turnstile(settings.amizone_url, sitekey)

                # Inject the token into RecaptchaToken field and set _QString to "test"
                # (this is how Amizone's Turnstile callback works)
                await page.evaluate(f"""
                    () => {{
                        // Set RecaptchaToken (where Amizone stores Turnstile token)
                        const recaptchaInput = document.getElementById('RecaptchaToken');
                        if (recaptchaInput) {{
                            recaptchaInput.value = '{token}';
                        }}

                        // Set _QString to "test" (required by Amizone's validation)
                        const qstringInput = document.querySelector('input[name="_QString"]');
                        if (qstringInput) {{
                            qstringInput.value = 'test';
                        }}

                        // Also set cf-turnstile-response if it exists
                        const cfInput = document.querySelector('input[name="cf-turnstile-response"]');
                        if (cfInput) {{
                            cfInput.value = '{token}';
                        }}
                    }}
                """)

                logger.info("Turnstile token injected")
                await asyncio.sleep(1)

            # Check for reCAPTCHA v2 (on login form)
            recaptcha_element = await page.query_selector('.g-recaptcha')

            if recaptcha_element:
                logger.info("reCAPTCHA v2 detected, solving with CapSolver...")

                # Extract reCAPTCHA sitekey
                recaptcha_sitekey = await recaptcha_element.get_attribute('data-sitekey')

                if recaptcha_sitekey:
                    logger.info(f"Using reCAPTCHA sitekey: {recaptcha_sitekey}")

                    # Solve with CapSolver
                    recaptcha_token = await self.capsolver.solve_recaptcha_v2(settings.amizone_url, recaptcha_sitekey)

                    # Inject the token
                    await page.evaluate(f"""
                        () => {{
                            const textarea = document.getElementById('g-recaptcha-response');
                            if (textarea) {{
                                textarea.innerHTML = '{recaptcha_token}';
                                textarea.value = '{recaptcha_token}';
                            }}
                        }}
                    """)

                    logger.info("reCAPTCHA token injected")
                    await asyncio.sleep(1)
                else:
                    logger.warning("reCAPTCHA detected but could not find sitekey")

            # Fill in login credentials
            logger.info(f"Filling login form for user: {username}")
            await page.fill('input[name="_UserName"]', username)
            await page.fill('input[name="_Password"]', password)

            # Submit the form
            logger.info("Submitting login form")
            await page.click('button[type="submit"]')

            # Wait for navigation or error
            try:
                await page.wait_for_url('**/Home', timeout=30000)
                logger.info("Login successful - redirected to Home")
            except Exception as e:
                # Check if we're still on login page (failed login)
                current_url = page.url
                if '/Login' in current_url or current_url == settings.amizone_url:
                    logger.error("Login failed - still on login page")
                    return LoginResponse(
                        success=False,
                        message="Invalid credentials or login failed"
                    )
                logger.info(f"Navigation completed to: {current_url}")

            # Extract cookies
            cookies = await context.cookies()
            cookie_dict = {cookie['name']: cookie['value'] for cookie in cookies}

            # Find session cookie (ASP.NET_SessionId)
            session_id = cookie_dict.get('ASP.NET_SessionId')

            logger.info(f"Login successful for {username}, extracted {len(cookies)} cookies")

            return LoginResponse(
                success=True,
                message="Login successful",
                cookies=cookie_dict,
                session_id=session_id
            )

        except Exception as e:
            logger.error(f"Login failed: {str(e)}", exc_info=True)
            return LoginResponse(
                success=False,
                message=f"Login failed: {str(e)}"
            )
        finally:
            await context.close()


# Global browser service
browser_service = BrowserLoginService()


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Lifespan context manager for startup and shutdown"""
    await browser_service.start()
    yield
    await browser_service.stop()


# Create FastAPI app
app = FastAPI(
    title="Amizone Browser Login Service",
    description="Browser-based login service with CAPTCHA solving",
    version="1.0.0",
    lifespan=lifespan
)


@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {"status": "healthy"}


@app.post("/login", response_model=LoginResponse)
async def login(request: LoginRequest):
    """
    Perform browser-based login with CAPTCHA solving
    Returns session cookies on success
    """
    try:
        result = await browser_service.login(request.username, request.password)

        if not result.success:
            raise HTTPException(status_code=401, detail=result.message)

        return result
    except Exception as e:
        logger.error(f"Login endpoint error: {str(e)}", exc_info=True)
        raise HTTPException(status_code=500, detail=str(e))


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=settings.port,
        log_level="info"
    )
