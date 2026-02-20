# Copyright 2025 Christopher O'Connell
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM node:20

ARG TZ
ENV TZ="$TZ"

ARG CLAUDE_CODE_VERSION=latest

# Add GitHub CLI official apt repository (Debian packages are too old)
RUN mkdir -p /etc/apt/keyrings && \
  wget -qO- https://cli.github.com/packages/githubcli-archive-keyring.gpg | tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null && \
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | tee /etc/apt/sources.list.d/github-cli.list > /dev/null

# Install basic development tools, iptables/ipset, tmux, Python, and Go
RUN apt-get update && apt-get install -y --no-install-recommends \
  less \
  git \
  procps \
  sudo \
  fzf \
  zsh \
  man-db \
  unzip \
  gnupg2 \
  gh \
  iptables \
  ipset \
  iproute2 \
  dnsutils \
  dnsmasq \
  aggregate \
  jq \
  nano \
  vim \
  tmux \
  python3 \
  python3-pip \
  python3-venv \
  curl \
  wget \
  build-essential \
  locales \
  ca-certificates \
  && apt-get clean && rm -rf /var/lib/apt/lists/*

# Set up UTF-8 locale
RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen && locale-gen
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

# Install Go 1.26.0 (architecture-aware)
RUN ARCH=$(dpkg --print-architecture) && \
  wget "https://go.dev/dl/go1.26.0.linux-${ARCH}.tar.gz" && \
  tar -C /usr/local -xzf "go1.26.0.linux-${ARCH}.tar.gz" && \
  rm "go1.26.0.linux-${ARCH}.tar.gz"

# Set Go environment variables
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/home/node/go"
ENV PATH="${GOPATH}/bin:${PATH}"

# Install Java JDK 17 (for Android development)
RUN ARCH=$(dpkg --print-architecture) && \
  if [ "$ARCH" = "amd64" ]; then JAVA_ARCH="x64"; elif [ "$ARCH" = "arm64" ]; then JAVA_ARCH="aarch64"; else JAVA_ARCH="$ARCH"; fi && \
  wget "https://download.java.net/java/GA/jdk17.0.2/dfd4a8d0985749f896bed50d7138ee7f/8/GPL/openjdk-17.0.2_linux-${JAVA_ARCH}_bin.tar.gz" && \
  tar -C /usr/local -xzf "openjdk-17.0.2_linux-${JAVA_ARCH}_bin.tar.gz" && \
  rm "openjdk-17.0.2_linux-${JAVA_ARCH}_bin.tar.gz"

# Set Java environment variables
ENV JAVA_HOME="/usr/local/jdk-17.0.2"
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# Install Gradle 8.13 (for Android development)
ARG GRADLE_VERSION=8.13
RUN wget "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" && \
  unzip -d /usr/local "gradle-${GRADLE_VERSION}-bin.zip" && \
  rm "gradle-${GRADLE_VERSION}-bin.zip"

# Set Gradle environment variables
ENV GRADLE_HOME="/usr/local/gradle-${GRADLE_VERSION}"
ENV PATH="${GRADLE_HOME}/bin:${PATH}"

# Install UV (Python package manager)
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.cargo/bin:${PATH}"

# Ensure default node user has access to /usr/local/share
RUN mkdir -p /usr/local/share/npm-global && \
  chown -R node:node /usr/local/share

ARG USERNAME=node

# Persist bash history
RUN SNIPPET="export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  && mkdir /commandhistory \
  && touch /commandhistory/.bash_history \
  && chown -R $USERNAME /commandhistory

# Set `DEVCONTAINER` environment variable to help with orientation
ENV DEVCONTAINER=true
ENV MAESTRO_CONTAINER=true

# Create workspace and config directories and set permissions
RUN mkdir -p /workspace /home/node/.claude /home/node/go \
  /home/node/.maestro/state /home/node/.maestro/logs /home/node/.maestro/pending-messages && \
  chown -R node:node /workspace /home/node/.claude /home/node/go /home/node/.maestro

WORKDIR /workspace

# Install git-delta for better diffs
ARG GIT_DELTA_VERSION=0.18.2
RUN ARCH=$(dpkg --print-architecture) && \
  wget "https://github.com/dandavison/delta/releases/download/${GIT_DELTA_VERSION}/git-delta_${GIT_DELTA_VERSION}_${ARCH}.deb" && \
  sudo dpkg -i "git-delta_${GIT_DELTA_VERSION}_${ARCH}.deb" && \
  rm "git-delta_${GIT_DELTA_VERSION}_${ARCH}.deb"

# Configure tmux
RUN echo "set -g mouse on\n\
set -g history-limit 50000\n\
set -g status-interval 5\n\
set -g status-left-length 50\n\
set -g status-right-length 50\n\
set-option -g allow-rename off\n\
set-option -g visual-bell off\n\
set-option -g bell-action other\n\
bind-key -n C-S-Left swap-window -t -1\n\
bind-key -n C-S-Right swap-window -t +1" > /etc/tmux.conf

# Set up non-root user
USER node

# Make UV available to node user
ENV PATH="/home/node/.cargo/bin:${PATH}"
RUN curl -LsSf https://astral.sh/uv/install.sh | sh

# Install global packages
ENV NPM_CONFIG_PREFIX=/usr/local/share/npm-global
ENV PATH=$PATH:/usr/local/share/npm-global/bin

# Set the default shell to zsh rather than sh
ENV SHELL=/bin/zsh

# Set the default editor and visual
ENV EDITOR=nano
ENV VISUAL=nano

# Default powerline10k theme
ARG ZSH_IN_DOCKER_VERSION=1.2.0
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
  -p git \
  -p fzf \
  -a "source /usr/share/doc/fzf/examples/key-bindings.zsh" \
  -a "source /usr/share/doc/fzf/examples/completion.zsh" \
  -a "export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  -a "export PATH=/usr/local/go/bin:\$GOPATH/bin:\$JAVA_HOME/bin:\$GRADLE_HOME/bin:\$PATH" \
  -a "export MAESTRO_CONTAINER=true" \
  -x

# Install Claude
RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}

# Configure git
RUN git config --global init.defaultBranch main && \
  git config --global user.email "maestro@container" && \
  git config --global user.name "Maestro Container"

# Build daemon-ipc binary (privilege-isolated IPC proxy)
COPY docker/daemon-ipc/main.go /tmp/daemon-ipc/main.go
USER root
RUN cd /tmp/daemon-ipc && \
  CGO_ENABLED=0 go build -o /usr/local/bin/daemon-ipc main.go && \
  rm -rf /tmp/daemon-ipc

# Create dedicated user for daemon IPC (iptables uid-owner isolation)
RUN useradd --system --no-create-home --shell /usr/sbin/nologin maestro-ipc

# Build maestro-request CLI
COPY docker/maestro-request-go/ /tmp/maestro-request/
RUN cd /tmp/maestro-request && \
  CGO_ENABLED=0 go build -o /usr/local/bin/maestro-request . && \
  rm -rf /tmp/maestro-request

# Build maestro-agent (container-side agent process manager)
COPY docker/maestro-agent/ /tmp/maestro-agent/
RUN cd /tmp/maestro-agent && \
  CGO_ENABLED=0 go build -o /usr/local/bin/maestro-agent . && \
  rm -rf /tmp/maestro-agent

# Copy scripts (init-firewall.sh is the single source of truth from assets/)
COPY assets/init-firewall.sh /usr/local/bin/
COPY docker/container-startup.sh /usr/local/bin/

RUN chmod +x /usr/local/bin/init-firewall.sh /usr/local/bin/container-startup.sh /usr/local/bin/maestro-request /usr/local/bin/daemon-ipc /usr/local/bin/maestro-agent && \
  echo "node ALL=(root) NOPASSWD: /usr/local/bin/init-firewall.sh" > /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /usr/sbin/dnsmasq" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /usr/bin/tee /etc/resolv.conf" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /bin/chown" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /bin/chmod" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /usr/sbin/update-ca-certificates" >> /etc/sudoers.d/node && \
  echo "node ALL=(maestro-ipc) NOPASSWD: /usr/local/bin/daemon-ipc" >> /etc/sudoers.d/node && \
  chmod 0440 /etc/sudoers.d/node

USER node

# Default command
CMD ["/usr/local/bin/container-startup.sh"]