# Copyright 2025 Christopher O'Connell
# All rights reserved

FROM node:20

ARG TZ
ENV TZ="$TZ"

ARG CLAUDE_CODE_VERSION=latest

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

# Install Go 1.25.2 (architecture-aware)
RUN ARCH=$(dpkg --print-architecture) && \
  wget "https://go.dev/dl/go1.25.2.linux-${ARCH}.tar.gz" && \
  tar -C /usr/local -xzf "go1.25.2.linux-${ARCH}.tar.gz" && \
  rm "go1.25.2.linux-${ARCH}.tar.gz"

# Set Go environment variables
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/home/node/go"
ENV PATH="${GOPATH}/bin:${PATH}"

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
ENV MCL_CONTAINER=true

# Create workspace and config directories and set permissions
RUN mkdir -p /workspace /home/node/.claude /home/node/go && \
  chown -R node:node /workspace /home/node/.claude /home/node/go

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
  -a "export PATH=/usr/local/go/bin:\$GOPATH/bin:\$PATH" \
  -a "export MCL_CONTAINER=true" \
  -x

# Install Claude
RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}

# Configure git
RUN git config --global init.defaultBranch main && \
  git config --global user.email "maestro@container" && \
  git config --global user.name "Maestro Container"

# Copy and set up firewall script
COPY init-firewall.sh /usr/local/bin/
COPY container-startup.sh /usr/local/bin/

USER root
RUN chmod +x /usr/local/bin/init-firewall.sh /usr/local/bin/container-startup.sh && \
  echo "node ALL=(root) NOPASSWD: /usr/local/bin/init-firewall.sh" > /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /usr/sbin/dnsmasq" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /usr/bin/tee /etc/resolv.conf" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /bin/chown" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /bin/chmod" >> /etc/sudoers.d/node && \
  echo "node ALL=(root) NOPASSWD: /usr/sbin/update-ca-certificates" >> /etc/sudoers.d/node && \
  chmod 0440 /etc/sudoers.d/node

USER node

# Default command
CMD ["/usr/local/bin/container-startup.sh"]