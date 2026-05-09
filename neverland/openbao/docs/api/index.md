# API Reference

This is the API reference for **APPNAME**. All endpoints require authentication unless marked as public.

!!! info "Base URL"
    ```
    https://<your-domain>/api/v1
    ```

## API Documentation Requirements

Every project must provide **two** API specification files in the `docs/api/` directory:

| File | Format | Purpose |
|------|--------|---------|
| `swagger.json` | Swagger 2.0 | Legacy tooling compatibility |
| `openapi.yaml` | OpenAPI 3.x | Primary specification for code generation, validation, and docs |

Both files must be kept in sync and describe the same API surface.

!!! note "MkDocs Integration"
    The `docs/` directory is cloned and placed directly into the MkDocs site during documentation builds. Write all documentation — including API specs — with this in mind. Paths, cross-references, and assets should work correctly when served under MkDocs.

## Authentication

All API requests must include a valid JWT token in the `Authorization` header:

```http
Authorization: Bearer <token>
```

See [Authentication](authentication.md) for details on obtaining tokens.

---

## Endpoints Overview

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | [`/auth/login`](#post-authlogin) | Authenticate and get a token |
| `GET` | [`/health`](#get-health) | Health check (public) |
| `GET` | [`/items`](#get-items) | List all items |
| `GET` | [`/items/:id`](#get-itemsid) | Get a single item |
| `POST` | [`/items`](#post-items) | Create an item |
| `PUT` | [`/items/:id`](#put-itemsid) | Update an item |
| `DELETE` | [`/items/:id`](#delete-itemsid) | Delete an item |

---

## `POST` /auth/login

Authenticate a user and receive a JWT token.

**Request:**

```json
{
  "username": "admin",
  "password": "secret"
}
```

**Response** `200 OK`:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-03-20T00:00:00Z"
}
```

**Errors:**

| Status | Description |
|--------|-------------|
| `401` | Invalid credentials |
| `429` | Rate limited — too many attempts |

---

## `GET` /health

Health check endpoint. No authentication required.

**Response** `200 OK`:

```json
{
  "status": "ok",
  "version": "1.0.0",
  "uptime": "48h32m"
}
```

---

## `GET` /items

List all items with optional filtering and pagination.

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | integer | `1` | Page number |
| `per_page` | integer | `20` | Items per page (max 100) |
| `sort` | string | `created_at` | Sort field: `created_at`, `updated_at`, `name` |
| `order` | string | `desc` | Sort order: `asc`, `desc` |
| `search` | string | — | Filter by name (partial match) |
| `status` | string | — | Filter by status: `active`, `archived` |

**Request:**

```http
GET /api/v1/items?page=1&per_page=10&status=active
Authorization: Bearer <token>
```

**Response** `200 OK`:

```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Example Item",
      "status": "active",
      "created_at": "2026-03-15T10:30:00Z",
      "updated_at": "2026-03-18T14:22:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "per_page": 10,
    "total": 42,
    "total_pages": 5
  }
}
```

---

## `GET` /items/:id

Get a single item by ID.

**Response** `200 OK`:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Example Item",
  "description": "A detailed description of the item.",
  "status": "active",
  "metadata": {
    "priority": "high",
    "tags": ["backend", "v2"]
  },
  "created_at": "2026-03-15T10:30:00Z",
  "updated_at": "2026-03-18T14:22:00Z"
}
```

**Errors:**

| Status | Description |
|--------|-------------|
| `404` | Item not found |

---

## `POST` /items

Create a new item.

**Request:**

```json
{
  "name": "New Item",
  "description": "Optional description",
  "metadata": {
    "priority": "medium",
    "tags": ["frontend"]
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Item name (1-255 chars) |
| `description` | string | no | Item description |
| `metadata` | object | no | Arbitrary key-value metadata |

**Response** `201 Created`:

```json
{
  "id": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "name": "New Item",
  "status": "active",
  "created_at": "2026-03-19T08:00:00Z"
}
```

**Errors:**

| Status | Description |
|--------|-------------|
| `400` | Validation error (missing name, name too long) |
| `409` | Duplicate name |

---

## `PUT` /items/:id

Update an existing item. Only provided fields are updated (partial update).

**Request:**

```json
{
  "name": "Updated Name",
  "status": "archived"
}
```

**Response** `200 OK`:

Returns the full updated item (same shape as GET /items/:id).

---

## `DELETE` /items/:id

Delete an item. This is a soft delete — the item is marked as `deleted` but can be recovered.

**Response** `204 No Content`

**Errors:**

| Status | Description |
|--------|-------------|
| `404` | Item not found |

---

## Error Format

All errors follow a consistent format:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Name is required",
    "details": [
      {
        "field": "name",
        "message": "must not be empty"
      }
    ]
  }
}
```

## Rate Limiting

| Endpoint | Limit |
|----------|-------|
| `POST /auth/login` | 5 requests / minute |
| All other endpoints | 100 requests / minute |

Rate limit headers are included in every response:

```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 97
X-RateLimit-Reset: 1710835200
```
