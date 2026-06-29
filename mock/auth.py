"""Authentication simulation: RSA key generation, JWT create/validate, OIDC endpoints."""

from __future__ import annotations

import base64
import time
from dataclasses import dataclass

import jwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi import Request
from starlette.middleware.base import BaseHTTPMiddleware, RequestResponseEndpoint
from starlette.responses import JSONResponse, Response

ALGORITHM = "RS256"
TOKEN_LIFETIME = 3600


@dataclass
class UserConfig:
    password: str
    tenants: list[str]
    roles: list[str]
    groups: list[str]


USERS: dict[str, UserConfig] = {
    "admin": UserConfig(
        password="admin",
        tenants=["*"],
        roles=["cloud-provider-admin"],
        groups=["admins"],
    ),
    "adam": UserConfig(
        password="adam",
        tenants=["engineering"],
        roles=["tenant-admin"],
        groups=["engineering"],
    ),
    "ben": UserConfig(
        password="ben",
        tenants=["engineering", "sales"],
        roles=["tenant-user"],
        groups=["engineering", "sales"],
    ),
    "charles": UserConfig(
        password="charles",
        tenants=["sales"],
        roles=["tenant-user"],
        groups=["sales"],
    ),
}

_NO_AUTH_PATHS = {
    "/.well-known/openid-configuration",
    "/.well-known/jwks.json",
    "/auth/token",
    "/api/fulfillment/v1/capabilities",
}


class AuthProvider:
    def __init__(self, issuer: str) -> None:
        self.issuer = issuer
        self._private_key = rsa.generate_private_key(
            public_exponent=65537, key_size=2048
        )
        self._public_key = self._private_key.public_key()
        self._kid = "mock-key-1"

    def create_token(
        self,
        username: str,
        organization: str | None = None,
    ) -> str:
        user = USERS.get(username)
        if not user:
            raise ValueError(f"Unknown user: {username}")

        if organization is None:
            organization = user.tenants[0] if user.tenants[0] != "*" else ""

        now = int(time.time())
        payload = {
            "iss": self.issuer,
            "sub": username,
            "iat": now,
            "exp": now + TOKEN_LIFETIME,
            "typ": "Bearer",
            "organization": organization,
            "groups": user.groups,
            "realm_access": {"roles": user.roles},
            "preferred_username": username,
        }
        return jwt.encode(
            payload,
            self._private_key,
            algorithm=ALGORITHM,
            headers={"kid": self._kid},
        )

    def validate_token(self, token: str) -> dict:
        return jwt.decode(
            token,
            self._public_key,
            algorithms=[ALGORITHM],
            options={"verify_aud": False},
        )

    def jwks(self) -> dict:
        pub_numbers = self._public_key.public_numbers()
        n_bytes = pub_numbers.n.to_bytes(
            (pub_numbers.n.bit_length() + 7) // 8, byteorder="big"
        )
        e_bytes = pub_numbers.e.to_bytes(
            (pub_numbers.e.bit_length() + 7) // 8, byteorder="big"
        )
        return {
            "keys": [
                {
                    "kty": "RSA",
                    "use": "sig",
                    "kid": self._kid,
                    "alg": ALGORITHM,
                    "n": base64.urlsafe_b64encode(n_bytes)
                    .rstrip(b"=")
                    .decode(),
                    "e": base64.urlsafe_b64encode(e_bytes)
                    .rstrip(b"=")
                    .decode(),
                }
            ]
        }

    def openid_configuration(self) -> dict:
        return {
            "issuer": self.issuer,
            "authorization_endpoint": f"{self.issuer}/auth/authorize",
            "token_endpoint": f"{self.issuer}/auth/token",
            "jwks_uri": f"{self.issuer}/.well-known/jwks.json",
            "response_types_supported": ["code", "token"],
            "subject_types_supported": ["public"],
            "id_token_signing_alg_values_supported": [ALGORITHM],
            "grant_types_supported": ["password", "client_credentials"],
        }

    def capabilities(self) -> dict:
        return {
            "authn": {
                "trusted_token_issuers": [self.issuer],
            }
        }


class AuthMiddleware(BaseHTTPMiddleware):
    def __init__(self, app, auth_provider: AuthProvider, no_auth: bool = False) -> None:
        super().__init__(app)
        self.auth_provider = auth_provider
        self.no_auth = no_auth

    async def dispatch(
        self, request: Request, call_next: RequestResponseEndpoint
    ) -> Response:
        if request.url.path in _NO_AUTH_PATHS:
            request.state.tenant = ""
            request.state.user = "anonymous"
            request.state.roles = []
            return await call_next(request)

        if self.no_auth:
            request.state.tenant = "engineering"
            request.state.user = "admin"
            request.state.roles = ["cloud-provider-admin"]
            return await call_next(request)

        auth_header = request.headers.get("authorization", "")
        if not auth_header.lower().startswith("bearer "):
            return JSONResponse(
                {"code": 16, "message": "Missing or invalid authorization header", "details": []},
                status_code=401,
            )

        token = auth_header[7:]
        try:
            claims = self.auth_provider.validate_token(token)
        except jwt.ExpiredSignatureError:
            return JSONResponse(
                {"code": 16, "message": "Token expired", "details": []},
                status_code=401,
            )
        except jwt.InvalidTokenError as e:
            return JSONResponse(
                {"code": 16, "message": f"Invalid token: {e}", "details": []},
                status_code=401,
            )

        request.state.user = claims.get("sub", "")
        request.state.tenant = claims.get("organization", "")
        request.state.roles = claims.get("realm_access", {}).get("roles", [])

        if "*" in USERS.get(request.state.user, UserConfig("", [], [], [])).tenants:
            request.state.tenant = "*"

        return await call_next(request)
