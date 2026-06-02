FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    build-essential \
    sudo \
    vim

# NOPASSWD sudo so `tomei apply --system` (Phase 2 privileged tools + Phase 4
# SystemPackage*) works inside the example container without an interactive
# prompt. Same configuration as the e2e test container at
# e2e/containers/ubuntu/Dockerfile.
RUN useradd -m -s /bin/bash lipnoise && \
    echo "lipnoise ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/lipnoise && \
    chown root:root /etc/sudoers.d/lipnoise && \
    chmod 0440 /etc/sudoers.d/lipnoise

COPY tomei /usr/local/bin/tomei
RUN chmod +x /usr/local/bin/tomei

USER lipnoise
WORKDIR /home/lipnoise

ENV PATH="/home/lipnoise/.local/bin:${PATH}"

COPY --chown=lipnoise:lipnoise minimal/ /home/lipnoise/examples/minimal/
COPY --chown=lipnoise:lipnoise real-world/ /home/lipnoise/examples/real-world/

CMD ["bash"]
