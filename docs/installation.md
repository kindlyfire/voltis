# Installation

Voltis has a single supported installation method, using the [Docker container](https://github.com/kindlyfire/voltis/pkgs/container/voltis).
This tutorial assumes you have [Docker](https://docs.docker.com/get-docker/) installed.

Here's an example Docker Compose file to get started:

```yml
services:
    app:
        image: ghcr.io/kindlyfire/voltis:1.0.0-alpha.3
        ports:
            - '127.0.0.1:8080:8080'
        environment:
            APP_DATABASE_URL: postgresql://postgres:postgres@postgres:5432/voltis
        depends_on:
            - postgres
        volumes:
            - /my/library/1:/app/library/1
            - /my/library/2:/app/library/2

    postgres:
        image: paradedb/paradedb:0.21.8-pg18
        environment:
            POSTGRES_DB: voltis
            POSTGRES_USER: postgres
            POSTGRES_PASSWORD: postgres
        volumes:
            - ./data_postgres:/var/lib/postgresql
        healthcheck:
            test: ['CMD-SHELL', 'pg_isready -U postgres']
            interval: 3s
            timeout: 3s
            retries: 10
```

::: warning
Voltis requires [ParadeDB](https://www.paradedb.com/) for full-text search. A
standard PostgreSQL image will not work. The image above includes it.
:::

Follow these steps to install:

```bash
# Pick a directory to install Voltis in
cd /path/to/install/dir

# Download the compose file
wget https://voltis.tijlvdb.me/compose.yml

# Edit the volume mounts to point to your libraries
nano compose.yml

# Bring up the containers
docker compose up -d
```

After the containers are up, you can access Voltis at `http://localhost:8080`.

## Creating an admin user

When no admin user exists yet, Voltis automatically enables the registration
page. The first user to register is granted admin permissions. Once the first
user is created, registration is disabled by default.

Alternatively, you can create an admin account through the CLI:

```bash
# Connect to the container
docker compose exec app sh
# Create the user
./voltis users create myuser --admin --password mypass123
```

## Adding libraries and scanning

Under "Settings" in the web interface, you can manage users and libraries, and
scan your libraries to add content.
