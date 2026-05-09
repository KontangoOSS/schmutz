# Authentication

APPNAME uses JWT bearer tokens for API authentication.

## Getting a Token

=== "cURL"

    ```bash
    curl -X POST https://your-domain/api/v1/auth/login \
      -H "Content-Type: application/json" \
      -d '{"username": "admin", "password": "secret"}'
    ```

=== "JavaScript"

    ```javascript
    const response = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username: 'admin',
        password: 'secret'
      })
    });
    const { token } = await response.json();
    ```

=== "Go"

    ```go
    payload := map[string]string{
        "username": "admin",
        "password": "secret",
    }
    body, _ := json.Marshal(payload)
    resp, err := http.Post(
        "https://your-domain/api/v1/auth/login",
        "application/json",
        bytes.NewBuffer(body),
    )
    ```

## Using the Token

Include the token in the `Authorization` header for all subsequent requests:

```http
GET /api/v1/items HTTP/1.1
Host: your-domain
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

## Token Lifecycle

```mermaid
sequenceDiagram
    participant Client
    participant API
    participant Auth

    Client->>API: POST /auth/login
    API->>Auth: Validate credentials
    Auth-->>API: User verified
    API-->>Client: JWT token (expires in 24h)

    Client->>API: GET /items (Bearer token)
    API->>Auth: Validate JWT
    Auth-->>API: Token valid
    API-->>Client: 200 OK + data

    Note over Client,Auth: After 24 hours...

    Client->>API: GET /items (expired token)
    API->>Auth: Validate JWT
    Auth-->>API: Token expired
    API-->>Client: 401 Unauthorized
```

## Token Format

Tokens are standard JWT with the following claims:

| Claim | Description |
|-------|-------------|
| `sub` | User ID (UUID) |
| `exp` | Expiration time (Unix timestamp) |
| `iat` | Issued at (Unix timestamp) |
| `role` | User role: `admin`, `member`, `viewer` |

??? example "Decoded token example"

    **Header:**
    ```json
    {
      "alg": "HS256",
      "typ": "JWT"
    }
    ```

    **Payload:**
    ```json
    {
      "sub": "550e8400-e29b-41d4-a716-446655440000",
      "exp": 1710921600,
      "iat": 1710835200,
      "role": "admin"
    }
    ```

## Permissions by Role

| Action | Admin | Member | Viewer |
|--------|:-----:|:------:|:------:|
| List items | :material-check: | :material-check: | :material-check: |
| View item details | :material-check: | :material-check: | :material-check: |
| Create items | :material-check: | :material-check: | :material-close: |
| Update items | :material-check: | :material-check: | :material-close: |
| Delete items | :material-check: | :material-close: | :material-close: |
| Manage users | :material-check: | :material-close: | :material-close: |

!!! warning "Security Notes"
    - Tokens expire after **24 hours** — there is no refresh token flow
    - Store tokens securely — never in `localStorage` for web apps
    - The `POST /auth/login` endpoint is rate-limited to **5 requests per minute**
    - All API traffic must use HTTPS in production
