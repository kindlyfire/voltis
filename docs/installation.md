# Installation

Voltis has a single supported installation method, using the [Docker container](https://github.com/kindlyfire/voltis/pkgs/container/voltis).

Here's an example Docker Compose file to get started:

```yml
services:
    app:
        image: ghcr.io/kindlyfire/voltis:dev
        ports:
            - '127.0.0.1:8000:8000'
        environment:
            APP_DB_URL: postgresql+psycopg://postgres:postgres@postgres:5432/voltis
            APP_REGISTRATION_ENABLED: 'true'
        depends_on:
            - postgres
        volumes:
            - /my/library/1:/app/library/1
            - /my/library/2:/app/library/2

    postgres:
        image: postgres:18
        environment:
            POSTGRES_DB: voltis
            POSTGRES_USER: postgres
            POSTGRES_PASSWORD: postgres
        volumes:
            - ./postgres_data/:/var/lib/postgresql
```

Follow these steps to install:

```bash
# Pick a directory to install Voltis in
cd /path/to/install/dir
# Create a compose.yml file with the above content
nano compose.yml
# Bring up the containers
docker compose up -d
```

After the containers are up, you can access Voltis at `http://localhost:8000`.

## Creating an admin user

You can create an admin account through the CLI:

```bash
# Connect to the container
docker compose exec app sh
# Create the user
./voltis users create myuser --admin --password mypass123
```

## Adding libraries and scanning

Under "Settings" in the web interface, you can manage users and libraries, and
scan your libraries to add content. It is possible to scan through the CLI as
well to get additional details:

```bash
# Connect to the container
docker compose exec app sh
# Scan libraries
./voltis devtools scan --library l_00000000
```

Where `l_00000000` is the library ID, which can be found in the URL when viewing
the library in the web interface.
