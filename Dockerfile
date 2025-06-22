FROM python:3.13-slim-bookworm

# Install UV
RUN pip install uv

# Keeps Python from generating .pyc files in the container
ENV PYTHONDONTWRITEBYTECODE=1

# Turns off buffering for easier container logging
ENV PYTHONUNBUFFERED=1

# Copy the project into the image
COPY . /app

# Sync the project into a new environment, using the frozen lockfile
WORKDIR /app
RUN uv sync --frozen

# RUN uv run main.py genrate-keys

CMD ["uv", "run", "main.py", "server"]
