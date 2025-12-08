#!/bin/bash
# Copyright 2025 Christopher O'Connell
# All rights reserved

set -euo pipefail
IFS=$'\n\t'

echo "Initializing firewall with dnsmasq..."

# 1. Extract Docker DNS info BEFORE any flushing
DOCKER_DNS_RULES=$(iptables-save -t nat | grep "127\.0\.0\.11" || true)

# Flush existing rules and delete existing ipsets
iptables -F
iptables -X
iptables -t nat -F
iptables -t nat -X
iptables -t mangle -F
iptables -t mangle -X
ipset destroy allowed-domains 2>/dev/null || true

# 2. Selectively restore ONLY internal Docker DNS resolution
if [ -n "$DOCKER_DNS_RULES" ]; then
    echo "Restoring Docker DNS rules..."
    iptables -t nat -N DOCKER_OUTPUT 2>/dev/null || true
    iptables -t nat -N DOCKER_POSTROUTING 2>/dev/null || true
    echo "$DOCKER_DNS_RULES" | while read -r rule; do
        if [[ -n "$rule" ]]; then
            # Extract the rule without the chain name
            rule_cmd=$(echo "$rule" | sed 's/^-A //')
            iptables -t nat -A $rule_cmd || true
        fi
    done
else
    echo "No Docker DNS rules to restore"
fi

# Create ipset with CIDR support
ipset create allowed-domains hash:net

# Read allowed domains from config file if it exists
DOMAINS_FILE="/etc/allowed-domains.txt"
if [ -f "$DOMAINS_FILE" ]; then
    echo "Reading allowed domains from $DOMAINS_FILE"
    ALLOWED_DOMAINS=$(cat "$DOMAINS_FILE")
else
    echo "Using default allowed domains"
    ALLOWED_DOMAINS="registry.npmjs.org
api.anthropic.com
github.com
pypi.org
files.pythonhosted.org
proxy.golang.org
sum.golang.org
go.googlesource.com
storage.googleapis.com
fonts.googleapis.com
fonts.gstatic.com
sentry.io
statsig.anthropic.com
statsig.com
marketplace.visualstudio.com
vscode.blob.core.windows.net
update.code.visualstudio.com"
fi

# Kill any existing dnsmasq processes
killall dnsmasq 2>/dev/null || true

# Generate dnsmasq configuration
echo "Configuring dnsmasq with domain whitelist..."
DNSMASQ_CONF="/tmp/dnsmasq-firewall.conf"
cat > "$DNSMASQ_CONF" <<EOF
# Listen only on localhost
listen-address=127.0.0.1
bind-interfaces

# Don't read /etc/resolv.conf or /etc/hosts
no-resolv
no-hosts

# Use Google DNS as upstream
server=8.8.8.8
server=8.8.4.4

# Don't forward plain names (without dots)
domain-needed

# Log queries for debugging (can be disabled later)
log-queries
log-facility=/tmp/dnsmasq.log

# Block everything by default with address=/#/
# This returns NXDOMAIN for all queries not explicitly whitelisted
address=/#/

EOF

# Add --ipset entries for each allowed domain
echo "$ALLOWED_DOMAINS" | while read -r domain; do
    [ -z "$domain" ] && continue
    echo "  Whitelisting $domain"
    # For each domain, add an ipset directive
    # This tells dnsmasq to add resolved IPs to our ipset
    echo "ipset=/$domain/allowed-domains" >> "$DNSMASQ_CONF"
    # Override the address=/#/ block for this domain by providing upstream server
    echo "server=/$domain/8.8.8.8" >> "$DNSMASQ_CONF"
done

# Add wildcard entries for GitHub (to catch subdomains)
echo "ipset=/.github.com/allowed-domains" >> "$DNSMASQ_CONF"
echo "server=/.github.com/8.8.8.8" >> "$DNSMASQ_CONF"
echo "ipset=/.githubusercontent.com/allowed-domains" >> "$DNSMASQ_CONF"
echo "server=/.githubusercontent.com/8.8.8.8" >> "$DNSMASQ_CONF"
echo "ipset=/.anthropic.com/allowed-domains" >> "$DNSMASQ_CONF"
echo "server=/.anthropic.com/8.8.8.8" >> "$DNSMASQ_CONF"

# Add wildcard entries for AWS (to catch region-specific subdomains like bedrock-runtime.eu-central-1.amazonaws.com)
echo "ipset=/.amazonaws.com/allowed-domains" >> "$DNSMASQ_CONF"
echo "server=/.amazonaws.com/8.8.8.8" >> "$DNSMASQ_CONF"
echo "ipset=/.awsapps.com/allowed-domains" >> "$DNSMASQ_CONF"
echo "server=/.awsapps.com/8.8.8.8" >> "$DNSMASQ_CONF"

# Configure internal DNS for corporate networks (Zscaler, VPN, etc.)
INTERNAL_DNS_FILE="/etc/internal-dns.txt"
INTERNAL_DOMAINS_FILE="/etc/internal-domains.txt"
if [ -f "$INTERNAL_DNS_FILE" ] && [ -f "$INTERNAL_DOMAINS_FILE" ]; then
    INTERNAL_DNS=$(cat "$INTERNAL_DNS_FILE")
    if [ -n "$INTERNAL_DNS" ]; then
        echo "Configuring internal DNS server: $INTERNAL_DNS"
        while read -r domain; do
            [ -z "$domain" ] && continue
            echo "  Routing $domain via internal DNS"
            echo "ipset=/$domain/allowed-domains" >> "$DNSMASQ_CONF"
            echo "server=/$domain/$INTERNAL_DNS" >> "$DNSMASQ_CONF"
            # Also add wildcard for subdomains
            echo "ipset=/.$domain/allowed-domains" >> "$DNSMASQ_CONF"
            echo "server=/.$domain/$INTERNAL_DNS" >> "$DNSMASQ_CONF"
        done < "$INTERNAL_DOMAINS_FILE"
    fi
elif [ -f "$INTERNAL_DNS_FILE" ]; then
    echo "Warning: Internal DNS configured but no internal domains specified"
fi

# Start dnsmasq
echo "Starting dnsmasq..."
dnsmasq --conf-file="$DNSMASQ_CONF"

# Update /etc/resolv.conf to use local dnsmasq
echo "nameserver 127.0.0.1" | tee /etc/resolv.conf > /dev/null

# Process GitHub API ranges and add them directly to ipset
# We do this because GitHub has many IPs and we want to ensure we catch them all
echo "Fetching GitHub IP ranges..."
gh_ranges=$(curl -s https://api.github.com/meta)
if [ -z "$gh_ranges" ]; then
    echo "WARNING: Failed to fetch GitHub IP ranges - GitHub access may be limited"
else
    if echo "$gh_ranges" | jq -e '.web and .api and .git' >/dev/null; then
        echo "Adding GitHub IP ranges to ipset..."
        while read -r cidr; do
            if [[ "$cidr" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/[0-9]{1,2}$ ]]; then
                echo "  Adding GitHub range $cidr"
                ipset add allowed-domains "$cidr" 2>/dev/null || echo "  Warning: Could not add $cidr"
            fi
        done < <(echo "$gh_ranges" | jq -r '(.web + .api + .git)[]' | aggregate -q 2>/dev/null || echo "$gh_ranges" | jq -r '(.web + .api + .git)[]')
    else
        echo "WARNING: GitHub API response format unexpected"
    fi
fi

# Get host IP from default route
HOST_IP=$(ip route | grep default | cut -d" " -f3)
if [ -z "$HOST_IP" ]; then
    echo "WARNING: Failed to detect host IP - host access may be limited"
    HOST_NETWORK="172.17.0.0/16"
else
    HOST_NETWORK=$(echo "$HOST_IP" | sed "s/\.[0-9]*$/.0\/24/")
    echo "Host network detected as: $HOST_NETWORK"
fi

# Set up iptables rules
echo "Configuring iptables..."

# Allow localhost (for dnsmasq)
iptables -A INPUT -i lo -j ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT

# Allow DNS queries to localhost (where dnsmasq is listening)
iptables -A OUTPUT -p udp --dport 53 -d 127.0.0.1 -j ACCEPT
iptables -A INPUT -p udp --sport 53 -s 127.0.0.1 -j ACCEPT

# Allow dnsmasq to query upstream DNS servers (8.8.8.8, etc.)
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A INPUT -p udp --sport 53 -j ACCEPT

# Allow SSH (if needed)
iptables -A OUTPUT -p tcp --dport 22 -j ACCEPT
iptables -A INPUT -p tcp --sport 22 -m state --state ESTABLISHED -j ACCEPT

# Allow host network access
iptables -A INPUT -s "$HOST_NETWORK" -j ACCEPT
iptables -A OUTPUT -d "$HOST_NETWORK" -j ACCEPT

# Allow established connections
iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT

# Allow only outbound traffic to IPs in our ipset
# (These IPs are added by dnsmasq when domains are resolved)
iptables -A OUTPUT -m set --match-set allowed-domains dst -j ACCEPT

# Set default policies to DROP
iptables -P INPUT DROP
iptables -P FORWARD DROP
iptables -P OUTPUT DROP

# Explicitly REJECT all other outbound traffic for immediate feedback
iptables -A OUTPUT -j REJECT --reject-with icmp-admin-prohibited

echo "Firewall configuration complete"

# Verify firewall rules
echo "Verifying firewall rules..."
if curl --connect-timeout 5 https://example.com >/dev/null 2>&1; then
    echo "ERROR: Firewall verification failed - was able to reach https://example.com"
    exit 1
else
    echo "✓ Firewall blocking works - unable to reach https://example.com"
fi

# Verify allowed access
echo "Testing DNS resolution and access to whitelisted domains..."
if ! curl --connect-timeout 10 https://api.github.com/zen >/dev/null 2>&1; then
    echo "WARNING: Unable to reach https://api.github.com - GitHub access may be limited"
else
    echo "✓ GitHub API access works"
fi

if ! curl --connect-timeout 10 https://api.anthropic.com >/dev/null 2>&1; then
    echo "WARNING: Unable to reach https://api.anthropic.com - Anthropic access may be limited"
else
    echo "✓ Anthropic API access works"
fi

echo ""
echo "Firewall initialization complete!"
echo "DNS queries are now filtered through dnsmasq (logs at /tmp/dnsmasq.log)"
echo "Only whitelisted domains can be resolved and accessed."
