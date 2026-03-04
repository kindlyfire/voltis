# Command-line reference

The Voltis binary is available inside the Docker container. You can run commands
directly:

```bash
docker compose exec app ./voltis <command>
```

Or open a shell first:

```bash
docker compose exec app sh
./voltis <command>
```

## Environment variables

| Variable                   | Default             | Description                                                                              |
| -------------------------- | ------------------- | ---------------------------------------------------------------------------------------- |
| `APP_DATABASE_URL`         | _(required)_        | PostgreSQL connection URL                                                                |
| `APP_PORT`                 | `8080`              | HTTP server port                                                                         |
| `APP_CACHE_DIR`            | `/tmp/voltis_cache` | Directory for cached cover images                                                        |
| `APP_REGISTRATION_ENABLED` | `false`             | Allow open user registration. If no accounts exist, one user will be allowed to register |
| `APP_STATIC_DIR`           | _(empty)_           | Path to frontend static files (set automatically in the Docker image)                    |

## Commands

### `server`

Starts the HTTP server. This is what the container runs by default.

```bash
./voltis server
```

### `users create`

Creates a new user.

```bash
./voltis users create <username> --password <password> [--admin]
```

- `--password` is required. Use `-` to read from stdin
- `--admin` grants admin permissions
- Passwords must be at least 8 characters

### `users update`

Updates an existing user.

```bash
./voltis users update <username> [--username <new>] [--password <new>] [--admin | --no-admin]
```

All flags are optional.
