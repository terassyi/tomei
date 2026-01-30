FROM ubuntu:24.04

# Install minimal dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates

# Create test user
RUN useradd -m -s /bin/bash testuser

# Copy toto binary
COPY toto /usr/local/bin/toto
RUN chmod +x /usr/local/bin/toto

# Setup directories
USER testuser
WORKDIR /home/testuser

# Create config directory
RUN mkdir -p /home/testuser/.config/toto \
    && mkdir -p /home/testuser/.local/bin \
    && mkdir -p /home/testuser/.local/share/toto

# Add ~/.local/bin to PATH
ENV PATH="/home/testuser/.local/bin:${PATH}"

# Copy test config
COPY --chown=testuser:testuser config/ /home/testuser/.config/toto/

# Default command
CMD ["toto", "version"]
