FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    build-essential

RUN useradd -m -s /bin/bash lipnoise

USER lipnoise
WORKDIR /home/lipnoise

RUN curl -fsSL https://raw.githubusercontent.com/terassyi/tomei/main/install.sh | sh

ENV PATH="/home/lipnoise/.local/bin:${PATH}"

COPY --chown=lipnoise:lipnoise manifests/ /home/lipnoise/manifests/

CMD ["bash"]
