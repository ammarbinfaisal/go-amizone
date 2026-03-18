import base64
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


GRAFANA_URL = os.environ.get("GRAFANA_URL", "http://grafana:3000").rstrip("/")
ADMIN_USER = os.environ.get("GRAFANA_ADMIN_USER", "admin")
ADMIN_PASSWORD = os.environ.get("GRAFANA_ADMIN_PASSWORD", "admin")
PUBLIC_USER = os.environ.get("GRAFANA_PUBLIC_USER", "public")
PUBLIC_PASSWORD = os.environ.get("GRAFANA_PUBLIC_PASSWORD", "public")


def auth_header(username: str, password: str) -> str:
    token = base64.b64encode(f"{username}:{password}".encode("utf-8")).decode("ascii")
    return f"Basic {token}"


def api_request(method: str, path: str, payload=None, authenticated: bool = True):
    url = f"{GRAFANA_URL}{path}"
    data = None
    headers = {}

    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"

    if authenticated:
        headers["Authorization"] = auth_header(ADMIN_USER, ADMIN_PASSWORD)

    request = urllib.request.Request(url, data=data, method=method, headers=headers)

    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            body = response.read().decode("utf-8") or "{}"
            return response.status, json.loads(body)
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8") if error.fp else ""
        try:
            parsed = json.loads(body) if body else {}
        except json.JSONDecodeError:
            parsed = {"message": body}
        return error.code, parsed


def wait_for_grafana():
    for _ in range(90):
        status, _ = api_request("GET", "/api/health", authenticated=False)
        if status == 200:
            return
        time.sleep(2)
    raise RuntimeError("Grafana did not become ready in time")


def ensure_public_user():
    lookup_path = f"/api/users/lookup?loginOrEmail={urllib.parse.quote(PUBLIC_USER)}"
    status, body = api_request("GET", lookup_path)

    if status == 404:
        status, body = api_request(
            "POST",
            "/api/admin/users",
            {
                "name": PUBLIC_USER,
                "email": f"{PUBLIC_USER}@local.invalid",
                "login": PUBLIC_USER,
                "password": PUBLIC_PASSWORD,
            },
        )
        if status not in {200, 201}:
            raise RuntimeError(f"Failed to create public user: {status} {body}")
        user_id = int(body["id"])
        print(f"Created Grafana user '{PUBLIC_USER}' with id {user_id}")
    elif status == 200:
        user_id = int(body["id"])
        print(f"Found Grafana user '{PUBLIC_USER}' with id {user_id}")
    else:
        raise RuntimeError(f"Failed to lookup public user: {status} {body}")

    status, body = api_request(
        "PUT",
        f"/api/admin/users/{user_id}/password",
        {"password": PUBLIC_PASSWORD},
    )
    if status not in {200, 204}:
        raise RuntimeError(f"Failed to reset public user password: {status} {body}")

    status, body = api_request(
        "PUT",
        f"/api/admin/users/{user_id}/permissions",
        {"isGrafanaAdmin": False},
    )
    if status not in {200, 204}:
        print(
            f"Warning: failed to remove Grafana admin flag from '{PUBLIC_USER}': {status} {body}",
            file=sys.stderr,
        )

    role_payload = {"role": "Viewer"}
    for method in ("PATCH", "PUT"):
        status, body = api_request(method, f"/api/org/users/{user_id}", role_payload)
        if status in {200, 204}:
            print(f"Ensured '{PUBLIC_USER}' has Viewer role")
            return

    raise RuntimeError(f"Failed to set Viewer role for '{PUBLIC_USER}': {status} {body}")


def main():
    wait_for_grafana()
    ensure_public_user()


if __name__ == "__main__":
    main()
