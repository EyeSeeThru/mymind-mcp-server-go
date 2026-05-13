#!/usr/bin/env python3
"""
MyMind MCP Server v1.1
Wraps the MyMind REST API as a stdio MCP server.
Works with any MCP-capable AI client.
"""

import json
import sys
import os
import base64
import hashlib
import hmac
import time
import argparse
import urllib.parse
import urllib.request
import urllib.error
from typing import Any, Optional

VERSION = "1.2.0"
BASE_URL = "https://api.mymind.com"
DEFAULT_KEY_PATH = os.path.expanduser("~/.mymind_mcp_access_key")
DEFAULT_CONFIG_PATH = os.path.expanduser("~/.mymind_mcp_config.yaml")


# ─── Credential Loading ────────────────────────────────────────────────────────

def load_access_key_from_file(path: str) -> str:
    """Read access key from a plain text file (single line)."""
    if not os.path.exists(path):
        raise RuntimeError(
            f"Access key not found at {path}.\n"
            "Create it with: echo 'YOUR_KEY' > ~/.mymind_mcp_access_key && chmod 600 ~/.mymind_mcp_access_key\n"
            "Or use --config to specify a YAML config file."
        )
    with open(path, "r") as f:
        key = f.read().strip()
    if not key:
        raise RuntimeError(f"Access key at {path} is empty.")
    return key


def load_access_key() -> tuple[str, str]:
    """
    Load access key from the first available source:
    1. MYMIND_ACCESS_KEY env var
    2. ~/.mymind_mcp_access_key file
    3. ~/.mymind_mcp_config.yaml file
    Returns (access_key, base_url).
    """
    base_url = BASE_URL

    # 1. Env var
    key = os.environ.get("MYMIND_ACCESS_KEY", "")
    if key:
        return key, base_url

    # 2. Default key file
    if os.path.exists(DEFAULT_KEY_PATH):
        with open(DEFAULT_KEY_PATH, "r") as f:
            key = f.read().strip()
        if key:
            return key, base_url

    # 3. YAML config
    if os.path.exists(DEFAULT_CONFIG_PATH):
        import yaml
        with open(DEFAULT_CONFIG_PATH, "r") as f:
            config = yaml.safe_load(f)
        key = config.get("access_key", "")
        base_url = config.get("base_url", BASE_URL)
        if key:
            return key, base_url

    raise RuntimeError(
        "No access key found. Set MYMIND_ACCESS_KEY env var, "
        "create ~/.mymind_mcp_access_key, or use --config."
    )


def split_access_key(key: str) -> tuple[str, str]:
    """
    Split 'kid.secret' format into (kid, secret).
    If no dot found, treat entire key as the secret and use 'default' as kid.
    """
    if "." in key:
        parts = key.split(".", 1)
        return parts[0], parts[1]
    return "default", key


# ─── JWT Signing ─────────────────────────────────────────────────────────────

def sign_token(kid: str, secret_b64: str, path: str, method: str) -> str:
    """Generate a signed JWT bound to request path and method."""
    import json as _json

    try:
        secret = base64.b64decode(secret_b64)
    except Exception:
        secret = secret_b64.encode()

    now = int(time.time())
    payload = {
        "path": path,
        "method": method.upper(),
        "iat": now,
        "exp": now + 300,  # 5 minutes
    }
    header = {"alg": "HS256", "typ": "JWT", "kid": kid}

    def _b64(data: dict) -> str:
        return base64.urlsafe_b64encode(_json.dumps(data).encode()).rstrip(b"=").decode()

    header_b64 = _b64(header)
    payload_b64 = _b64(payload)
    message = f"{header_b64}.{payload_b64}"
    sig = hmac.new(secret, message.encode(), hashlib.sha256).digest()
    sig_b64 = base64.urlsafe_b64encode(sig).rstrip(b"=").decode()
    return f"{message}.{sig_b64}"


# ─── API Client ───────────────────────────────────────────────────────────────

class MyMindClient:
    def __init__(self, access_key: str, base_url: str = BASE_URL):
        self.access_key = access_key
        self.base_url = base_url
        self.kid, self.secret = split_access_key(access_key)

    def _request(
        self,
        method: str,
        path: str,
        body: Optional[dict] = None,
        params: Optional[dict] = None,
        headers: Optional[dict] = None,
    ) -> dict:
        """Make an authenticated request to the MyMind API."""
        url = self.base_url + path
        if params:
            encoded_params = {k: urllib.parse.quote(str(v), safe="") for k, v in params.items()}
            qs = "&".join(f"{k}={v}" for k, v in encoded_params.items())
            url = f"{url}?{qs}"

        data = json.dumps(body).encode() if body else None
        token = sign_token(self.kid, self.secret, path, method)

        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Authorization", f"Bearer {token}")
        req.add_header("Content-Type", "application/json")
        req.add_header("Accept", "application/json")
        req.add_header("User-Agent", f"mymind-mcp-server/{VERSION}")

        if headers:
            for k, v in headers.items():
                req.add_header(k, v)

        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                raw = resp.read()
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as e:
            raw = e.read() or b"{}"
            err = json.loads(raw)
            raise RuntimeError(f"MyMind API error {e.code}: {err}")
        except urllib.error.URLError as e:
            raise RuntimeError(f"Network error: {e.reason}")

    def _raw_request(
        self,
        method: str,
        path: str,
        headers: Optional[dict] = None,
    ) -> tuple[int, bytes, dict]:
        """Make an authenticated request and return raw response for streaming/blob."""
        url = self.base_url + path
        token = sign_token(self.kid, self.secret, path, method)

        req = urllib.request.Request(url, method=method)
        req.add_header("Authorization", f"Bearer {token}")
        req.add_header("User-Agent", f"mymind-mcp-server/{VERSION}")

        if headers:
            for k, v in headers.items():
                req.add_header(k, v)

        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                raw = resp.read()
                return resp.status, raw, dict(resp.headers)
        except urllib.error.HTTPError as e:
            raw = e.read() or b"{}"
            raise RuntimeError(f"MyMind API error {e.code}: {json.loads(raw)}")
        except urllib.error.URLError as e:
            raise RuntimeError(f"Network error: {e.reason}")

    # ─── Objects ────────────────────────────────────────────────────────────

    def list_objects(
        self,
        q: Optional[str] = None,
        limit: int = 100,
        content_as: Optional[str] = None,
        similar_to: Optional[str] = None,
    ) -> list[dict]:
        params: dict = {"limit": limit}
        if q:
            params["q"] = q
        if similar_to:
            params["similarTo"] = similar_to
        headers = {}
        if content_as:
            params["contentAs"] = content_as
            headers["Accept"] = "application/json"
        return self._request("GET", "/objects", params=params, headers=headers if headers else None)

    def create_object(
        self,
        title: Optional[str] = None,
        content: Optional[str] = None,
        url: Optional[str] = None,
        tags: Optional[list[str]] = None,
        spaces: Optional[list[str]] = None,
    ) -> dict:
        body: dict = {}
        if title:
            body["title"] = title
        if content:
            body["content"] = content
        if url:
            body["url"] = url
        if tags:
            body["tags"] = [{"name": t} for t in tags]
        if spaces:
            body["spaces"] = [{"id": s} for s in spaces]
        return self._request("POST", "/objects", body=body)

    def get_object(
        self,
        object_id: str,
        content_as: Optional[str] = None,
    ) -> dict:
        params = {}
        if content_as:
            params["contentAs"] = content_as
        return self._request(
            "GET",
            f"/objects/{object_id}",
            params=params if params else None,
        )

    def update_object(
        self,
        object_id: str,
        title: Optional[str] = None,
        summary: Optional[str] = None,
    ) -> dict:
        body = {}
        if title is not None:
            body["title"] = title
        if summary is not None:
            body["summary"] = summary
        return self._request("PATCH", f"/objects/{object_id}", body=body)

    def delete_object(self, object_id: str) -> dict:
        return self._request("DELETE", f"/objects/{object_id}")

    def restore_object(self, object_id: str) -> dict:
        return self._request("POST", f"/objects/{object_id}/restore")

    def download_object(self, object_id: str) -> dict:
        """Download original uploaded bytes. May 302-redirect to CDN."""
        status, raw, headers = self._raw_request("GET", f"/objects/{object_id}/blob")
        content_type = headers.get("Content-Type", "application/octet-stream")
        data_b64 = base64.b64encode(raw).decode()
        return {"content_type": content_type, "data": data_b64}

    def get_blob(self, object_id: str) -> dict:
        """Alias for download_object — /blob is the canonical path."""
        return self.download_object(object_id)

    def get_content(
        self,
        object_id: str,
        accept: str = "text/plain",
    ) -> dict:
        """
        Get raw content of an object.
        accept: 'text/plain', 'text/markdown', 'application/prose+json', 'text/html'
        Returns raw text (not wrapped in a Content object).
        """
        headers = {"Accept": accept}
        status, raw, resp_headers = self._raw_request(
            "GET", f"/objects/{object_id}/content", headers=headers
        )
        return {
            "content_type": resp_headers.get("Content-Type", accept),
            "content": raw.decode("utf-8", errors="replace"),
        }

    def get_thumbnail(self, object_id: str, size: Optional[str] = None) -> dict:
        """Get preview image. May 302-redirect to signed CDN URL."""
        params = {}
        if size:
            params["size"] = size
        path = f"/objects/{object_id}/thumbnail"
        status, raw, headers = self._raw_request(
            "GET",
            path + ("?" + urllib.parse.urlencode(params) if params else ""),
        )
        content_type = headers.get("Content-Type", "application/octet-stream")
        data_b64 = base64.b64encode(raw).decode()
        return {"content_type": content_type, "data": data_b64}

    def get_screenshot(self, object_id: str) -> dict:
        """Get screenshot captured at save time. May 302-redirect to CDN."""
        path = f"/objects/{object_id}/screenshot"
        status, raw, headers = self._raw_request("GET", path)
        content_type = headers.get("Content-Type", "application/octet-stream")
        data_b64 = base64.b64encode(raw).decode()
        return {"content_type": content_type, "data": data_b64}

    # ─── Pin ────────────────────────────────────────────────────────────────

    def pin_object(self, object_id: str, position: Optional[int] = None) -> dict:
        body = {}
        if position is not None:
            body["position"] = position
        return self._request("POST", f"/objects/{object_id}/pin", body=body if body else None)

    def unpin_object(self, object_id: str) -> dict:
        return self._request("DELETE", f"/objects/{object_id}/pin")

    # ─── Notes ──────────────────────────────────────────────────────────────

    def create_note(
        self,
        object_id: str,
        content: str,
        content_type: str = "text/markdown",
    ) -> dict:
        """Append a note to an object. Returns { id }."""
        headers = {"Content-Type": content_type}
        body = {"content": content} if content_type == "text/markdown" else content
        return self._request("POST", f"/objects/{object_id}/notes", body=body, headers=headers)

    def update_note(
        self,
        object_id: str,
        note_id: str,
        content: str,
        content_type: str = "text/markdown",
    ) -> dict:
        """Replace a note's body. Full replace."""
        headers = {"Content-Type": content_type}
        body = {"content": content} if content_type == "text/markdown" else content
        return self._request("PUT", f"/objects/{object_id}/notes/{note_id}", body=body, headers=headers)

    def delete_note(self, object_id: str, note_id: str) -> dict:
        """Remove a note from an object."""
        return self._request("DELETE", f"/objects/{object_id}/notes/{note_id}")

    # ─── Search ─────────────────────────────────────────────────────────────

    def search(
        self,
        query: str,
        limit: int = 20,
        semantic: bool = False,
        semantic_boost: Optional[float] = None,
        similar_to: Optional[str] = None,
        rerank: bool = False,
    ) -> list[dict]:
        params: dict = {"q": query, "limit": limit}
        if semantic:
            params["semantic"] = "true"
        if semantic_boost is not None:
            params["semanticBoost"] = str(semantic_boost)
        if similar_to:
            params["similarTo"] = similar_to
            params["semantic"] = "true"
        if rerank:
            params["rerank"] = "true"
            params["semantic"] = "true"
        return self._request("GET", "/search", params=params)

    # ─── Tags ───────────────────────────────────────────────────────────────

    def add_tags(self, object_id: str, tags: list[str]) -> dict:
        body = [{"name": t} for t in tags]
        return self._request("POST", f"/objects/{object_id}/tags", body=body)

    def remove_tags(self, object_id: str, tags: list[dict]) -> dict:
        """Remove tags. Body: [{ name: string }] or [{ id: Uid }] — mix allowed."""
        return self._request("DELETE", f"/objects/{object_id}/tags", body=tags)

    # ─── Spaces ─────────────────────────────────────────────────────────────

    def list_spaces(self) -> list[dict]:
        return self._request("GET", "/spaces")

    def create_space(self, name: str, color: Optional[str] = None) -> dict:
        body: dict = {"name": name}
        if color:
            body["color"] = color
        return self._request("POST", "/spaces", body=body)

    def get_space(self, space_id: str) -> dict:
        return self._request("GET", f"/spaces/{space_id}")

    def update_space(self, space_id: str, name: Optional[str] = None, color: Optional[str] = None) -> dict:
        body = {}
        if name is not None:
            body["name"] = name
        if color is not None:
            body["color"] = color
        return self._request("PATCH", f"/spaces/{space_id}", body=body)

    def delete_space(self, space_id: str) -> dict:
        return self._request("DELETE", f"/spaces/{space_id}")

    def add_object_to_space(self, space_id: str, object_id: str) -> dict:
        return self._request("PUT", f"/spaces/{space_id}/objects/{object_id}")

    def remove_object_from_space(self, space_id: str, object_id: str) -> dict:
        return self._request("DELETE", f"/spaces/{space_id}/objects/{object_id}")

    # ─── Links ──────────────────────────────────────────────────────────────

    def list_links(self) -> list[dict]:
        return self._request("GET", "/links")

    def create_link(self, source_id: str, target_id: str) -> dict:
        """Create a Manual link between two objects. Returns { id }."""
        return self._request("POST", "/links", body={"sourceId": source_id, "targetId": target_id})

    def delete_link(self, link_id: str) -> dict:
        """Delete a Manual link by ID. WikiLinks return 422."""
        return self._request("DELETE", f"/links/{link_id}")

    # ─── Tags ───────────────────────────────────────────────────────────────

    def list_tags(self, limit: int = 1000) -> list[dict]:
        return self._request("GET", "/tags", params={"limit": limit})

    # ─── Convert ────────────────────────────────────────────────────────────

    def convert(self, content: str, from_type: str, to_type: str) -> dict:
        """
        Convert content between formats.
        from_type / to_type: 'text/plain', 'text/markdown', 'application/prose+json'
        """
        headers = {
            "Content-Type": from_type,
            "Accept": to_type,
        }
        body = content if from_type == "text/plain" else json.dumps(content)
        return self._request("POST", "/convert", headers=headers)


# ─── MCP Protocol ─────────────────────────────────────────────────────────────

def tool_to_response(tool_name: str, result: Any) -> dict:
    return {
        "jsonrpc": "2.0",
        "id": None,
        "result": {
            "tool": tool_name,
            "content": [{"type": "text", "text": json.dumps(result, indent=2)}],
        },
    }


def error_to_response(error_msg: str) -> dict:
    return {
        "jsonrpc": "2.0",
        "id": None,
        "error": {"code": -32600, "message": error_msg},
    }


def handle_request(client: MyMindClient, method: str, params: dict) -> dict:
    """Route a method call to the appropriate client method."""
    try:
        if method == "list_objects":
            return tool_to_response(
                "list_objects",
                client.list_objects(
                    q=params.get("q"),
                    limit=params.get("limit", 100),
                    content_as=params.get("contentAs"),
                    similar_to=params.get("similarTo"),
                ),
            )

        elif method == "create_object":
            return tool_to_response(
                "create_object",
                client.create_object(
                    title=params.get("title"),
                    content=params.get("content"),
                    url=params.get("url"),
                    tags=params.get("tags"),
                    spaces=params.get("spaces"),
                ),
            )

        elif method == "get_object":
            return tool_to_response(
                "get_object",
                client.get_object(params["id"], content_as=params.get("contentAs")),
            )

        elif method == "get_content":
            return tool_to_response(
                "get_content",
                client.get_content(params["id"], accept=params.get("accept", "text/plain")),
            )

        elif method == "update_object":
            return tool_to_response(
                "update_object",
                client.update_object(
                    params["id"],
                    title=params.get("title"),
                    summary=params.get("summary"),
                ),
            )

        elif method == "delete_object":
            return tool_to_response(
                "delete_object",
                client.delete_object(params["id"]),
            )

        elif method == "restore_object":
            return tool_to_response(
                "restore_object",
                client.restore_object(params["id"]),
            )

        elif method == "pin_object":
            return tool_to_response(
                "pin_object",
                client.pin_object(params["id"], position=params.get("position")),
            )

        elif method == "unpin_object":
            return tool_to_response(
                "unpin_object",
                client.unpin_object(params["id"]),
            )

        elif method == "download_object":
            return tool_to_response(
                "download_object",
                client.download_object(params["id"]),
            )

        elif method == "get_thumbnail":
            return tool_to_response(
                "get_thumbnail",
                client.get_thumbnail(params["id"], size=params.get("size")),
            )

        elif method == "get_screenshot":
            return tool_to_response(
                "get_screenshot",
                client.get_screenshot(params["id"]),
            )

        elif method == "search":
            return tool_to_response(
                "search",
                client.search(
                    query=params["query"],
                    limit=params.get("limit", 20),
                    semantic=params.get("semantic", False),
                    semantic_boost=params.get("semanticBoost"),
                    similar_to=params.get("similarTo"),
                    rerank=params.get("rerank", False),
                ),
            )

        elif method == "add_tags":
            return tool_to_response(
                "add_tags",
                client.add_tags(params["id"], params["tags"]),
            )

        elif method == "remove_tags":
            return tool_to_response(
                "remove_tags",
                client.remove_tags(params["id"], params["tags"]),
            )

        elif method == "create_note":
            return tool_to_response(
                "create_note",
                client.create_note(
                    params["objectId"],
                    params["content"],
                    content_type=params.get("contentType", "text/markdown"),
                ),
            )

        elif method == "update_note":
            return tool_to_response(
                "update_note",
                client.update_note(
                    params["objectId"],
                    params["noteId"],
                    params["content"],
                    content_type=params.get("contentType", "text/markdown"),
                ),
            )

        elif method == "delete_note":
            return tool_to_response(
                "delete_note",
                client.delete_note(params["objectId"], params["noteId"]),
            )

        elif method == "list_spaces":
            return tool_to_response("list_spaces", client.list_spaces())

        elif method == "create_space":
            return tool_to_response(
                "create_space",
                client.create_space(
                    params["name"],
                    color=params.get("color"),
                ),
            )

        elif method == "get_space":
            return tool_to_response(
                "get_space",
                client.get_space(params["id"]),
            )

        elif method == "update_space":
            return tool_to_response(
                "update_space",
                client.update_space(
                    params["id"],
                    name=params.get("name"),
                    color=params.get("color"),
                ),
            )

        elif method == "delete_space":
            return tool_to_response(
                "delete_space",
                client.delete_space(params["id"]),
            )

        elif method == "add_object_to_space":
            return tool_to_response(
                "add_object_to_space",
                client.add_object_to_space(params["spaceId"], params["objectId"]),
            )

        elif method == "remove_object_from_space":
            return tool_to_response(
                "remove_object_from_space",
                client.remove_object_from_space(params["spaceId"], params["objectId"]),
            )

        elif method == "list_links":
            return tool_to_response("list_links", client.list_links())

        elif method == "create_link":
            return tool_to_response(
                "create_link",
                client.create_link(params["sourceId"], params["targetId"]),
            )

        elif method == "delete_link":
            return tool_to_response(
                "delete_link",
                client.delete_link(params["id"]),
            )

        elif method == "list_tags":
            return tool_to_response(
                "list_tags",
                client.list_tags(limit=params.get("limit", 1000)),
            )

        elif method == "convert":
            return tool_to_response(
                "convert",
                client.convert(
                    params["content"],
                    params["from"],
                    params["to"],
                ),
            )

        else:
            return error_to_response(f"Unknown tool: {method}")

    except Exception as e:
        return error_to_response(str(e))


# ─── Stdio Transport ───────────────────────────────────────────────────────────

TOOLS = [
    # Objects
    {
        "name": "list_objects",
        "description": "List objects from MyMind. Params: q (search), limit (default 100), contentAs (e.g. text/markdown), similarTo (rank by similarity to object ID).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "q": {"type": "string"},
                "limit": {"type": "integer", "default": 100},
                "contentAs": {"type": "string"},
                "similarTo": {"type": "string"},
            },
        },
    },
    {
        "name": "create_object",
        "description": "Create a new object (URL, note, or content) in MyMind.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "title": {"type": "string"},
                "content": {"type": "string"},
                "url": {"type": "string"},
                "tags": {"type": "array", "items": {"type": "string"}},
                "spaces": {"type": "array", "items": {"type": "string"}},
            },
        },
    },
    {
        "name": "get_object",
        "description": "Get a single object by ID. Optional: contentAs for format conversion.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "contentAs": {"type": "string"},
            },
            "required": ["id"],
        },
    },
    {
        "name": "get_content",
        "description": "Get raw object content (not wrapped). Params: id, accept (text/plain | text/markdown | application/prose+json | text/html).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "accept": {"type": "string", "default": "text/plain"},
            },
            "required": ["id"],
        },
    },
    {
        "name": "update_object",
        "description": "Update an object's title or summary.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "title": {"type": "string"},
                "summary": {"type": "string"},
            },
            "required": ["id"],
        },
    },
    {
        "name": "delete_object",
        "description": "Soft-delete an object. Recoverable for 30 days via restore_object.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    {
        "name": "restore_object",
        "description": "Restore a soft-deleted object within the 30-day recovery window.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    {
        "name": "pin_object",
        "description": "Pin an object to top of mind. Optional: position (zero-based slot).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "position": {"type": "integer"},
            },
            "required": ["id"],
        },
    },
    {
        "name": "unpin_object",
        "description": "Unpin an object.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    {
        "name": "download_object",
        "description": "Download original uploaded bytes. Returns base64. May 302-redirect to CDN.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    {
        "name": "get_thumbnail",
        "description": "Get preview image. Optional: size as WxH bounding box. May 302-redirect to CDN.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "size": {"type": "string"},
            },
            "required": ["id"],
        },
    },
    {
        "name": "get_screenshot",
        "description": "Get screenshot captured at save time (rendered webpage view). May 302-redirect to CDN.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    # Notes
    {
        "name": "create_note",
        "description": "Append a note to an object. Returns { id }. Default contentType: text/markdown.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "objectId": {"type": "string"},
                "content": {"type": "string"},
                "contentType": {"type": "string", "default": "text/markdown"},
            },
            "required": ["objectId", "content"],
        },
    },
    {
        "name": "update_note",
        "description": "Replace a note's body. Full replace, not incremental.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "objectId": {"type": "string"},
                "noteId": {"type": "string"},
                "content": {"type": "string"},
                "contentType": {"type": "string", "default": "text/markdown"},
            },
            "required": ["objectId", "noteId", "content"],
        },
    },
    {
        "name": "delete_note",
        "description": "Remove a note from an object. Idempotent.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "objectId": {"type": "string"},
                "noteId": {"type": "string"},
            },
            "required": ["objectId", "noteId"],
        },
    },
    # Search
    {
        "name": "search",
        "description": "Search MyMind objects. Params: query (required), limit, semantic, semanticBoost, similarTo, rerank.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "query": {"type": "string"},
                "limit": {"type": "integer", "default": 20},
                "semantic": {"type": "boolean", "default": False},
                "semanticBoost": {"type": "number"},
                "similarTo": {"type": "string"},
                "rerank": {"type": "boolean", "default": False},
            },
            "required": ["query"],
        },
    },
    # Tags
    {
        "name": "add_tags",
        "description": "Add tags to an object.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "tags": {"type": "array", "items": {"type": "string"}},
            },
            "required": ["id", "tags"],
        },
    },
    {
        "name": "remove_tags",
        "description": "Remove tags from an object. Body: [{ name: string }] or [{ id: Uid }], mix allowed.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "tags": {"type": "array", "items": {"type": "object"}},
            },
            "required": ["id", "tags"],
        },
    },
    {
        "name": "list_tags",
        "description": "List all tags in your mind.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "limit": {"type": "integer", "default": 1000},
            },
        },
    },
    # Spaces
    {
        "name": "list_spaces",
        "description": "List all spaces.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "create_space",
        "description": "Create a new space. Optional: color (CSS color value).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "name": {"type": "string"},
                "color": {"type": "string"},
            },
            "required": ["name"],
        },
    },
    {
        "name": "get_space",
        "description": "Get a space by ID.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    {
        "name": "update_space",
        "description": "Update a space's name or color.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "id": {"type": "string"},
                "name": {"type": "string"},
                "color": {"type": "string"},
            },
            "required": ["id"],
        },
    },
    {
        "name": "delete_space",
        "description": "Delete a space. Objects in the space survive without the membership.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    {
        "name": "add_object_to_space",
        "description": "Add an object to a space. Idempotent.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "spaceId": {"type": "string"},
                "objectId": {"type": "string"},
            },
            "required": ["spaceId", "objectId"],
        },
    },
    {
        "name": "remove_object_from_space",
        "description": "Remove an object from a space. Idempotent.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "spaceId": {"type": "string"},
                "objectId": {"type": "string"},
            },
            "required": ["spaceId", "objectId"],
        },
    },
    # Links
    {
        "name": "list_links",
        "description": "List all links (WikiLink and Manual) in your mind.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "create_link",
        "description": "Create a Manual link between two objects. Returns { id }. 201=new, 200=already exists.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "sourceId": {"type": "string"},
                "targetId": {"type": "string"},
            },
            "required": ["sourceId", "targetId"],
        },
    },
    {
        "name": "delete_link",
        "description": "Delete a Manual link by ID. WikiLinks return 422 — edit source note instead.",
        "inputSchema": {
            "type": "object",
            "properties": {"id": {"type": "string"}},
            "required": ["id"],
        },
    },
    # Convert
    {
        "name": "convert",
        "description": "Convert content between text/plain, text/markdown, and application/prose+json.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "content": {"type": "string"},
                "from": {"type": "string", "enum": ["text/plain", "text/markdown", "application/prose+json"]},
                "to": {"type": "string", "enum": ["text/plain", "text/markdown", "application/prose+json"]},
            },
            "required": ["content", "from", "to"],
        },
    },
]


def main():
    parser = argparse.ArgumentParser(description="MyMind MCP Server v1.1")
    parser.add_argument("--config", help="Path to YAML config file")
    parser.add_argument("--key-file", help="Path to plain text access key file")
    args = parser.parse_args()

    # Load credentials
    try:
        if args.config:
            access_key = load_access_key_from_config(args.config)
            base_url = BASE_URL
        elif args.key_file:
            access_key = load_access_key_from_file(args.key_file)
            base_url = BASE_URL
        else:
            access_key, base_url = load_access_key()
    except RuntimeError as e:
        sys.stderr.write(f"FATAL: {e}\n")
        sys.exit(1)

    client = MyMindClient(access_key, base_url)

    # Announce capabilities on startup
    capabilities = {
        "jsonrpc": "2.0",
        "id": None,
        "result": {
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "mymind", "version": VERSION},
        },
    }
    print(json.dumps(capabilities), flush=True)

    # Read requests from stdin
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue

        method = req.get("method", "")
        params = req.get("params", {}) or {}

        if method == "initialize":
            resp = {
                "jsonrpc": "2.0",
                "id": req.get("id"),
                "result": {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "mymind", "version": VERSION},
                },
            }
            print(json.dumps(resp), flush=True)

        elif method == "tools/list":
            resp = {
                "jsonrpc": "2.0",
                "id": req.get("id"),
                "result": {"tools": TOOLS},
            }
            print(json.dumps(resp), flush=True)

        elif method == "tools/call":
            tool_name = params.get("name", "")
            tool_args = params.get("arguments", {}) or {}
            resp = handle_request(client, tool_name, tool_args)
            resp["id"] = req.get("id")
            print(json.dumps(resp), flush=True)

        else:
            resp = error_to_response(f"Unsupported method: {method}")
            resp["id"] = req.get("id")
            print(json.dumps(resp), flush=True)


if __name__ == "__main__":
    main()