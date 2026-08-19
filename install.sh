#!/usr/bin/env bash
#
# One-command setup for the VPS Dashboard.
#
#   git clone https://github.com/Wayy01/vps-dashboard.git
#   cd vps-dashboard && sudo ./install.sh
#
# It asks a handful of questions, writes .env, builds the stack and leaves you
# with a dashboard you can actually reach. The questions that matter are about
# reachability: this thing is root-equivalent, so where it listens is the most
# consequential decision in the whole install, and the script will not make it
# quietly on your behalf.

set -euo pipefail

# ── output ──────────────────────────────────────────────────────────────────

if [ -t 1 ] && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; then
	BOLD=$(tput bold); DIM=$(tput dim); RESET=$(tput sgr0)
	BLUE=$(tput setaf 4); GREEN=$(tput setaf 2); YELLOW=$(tput setaf 3); RED=$(tput setaf 1)
else
	BOLD=""; DIM=""; RESET=""; BLUE=""; GREEN=""; YELLOW=""; RED=""
fi

say()  { printf '%s\n' "$*"; }
step() { printf '\n%s==>%s %s%s%s\n' "$BLUE" "$RESET" "$BOLD" "$*" "$RESET"; }
ok()   { printf '  %s✓%s %s\n' "$GREEN" "$RESET" "$*"; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$RESET" "$*"; }
die()  { printf '\n%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

# Prompts read plain stdin rather than /dev/tty. This script requires a cloned
# repository to run in, so it is never piped from curl — and reading stdin is
# what lets the whole flow be exercised non-interactively in a test.
#
# ask <prompt> <default> -> echoes the answer
ask() {
	local prompt="$1" default="${2:-}" reply
	if [ -n "$default" ]; then
		read -r -p "  $prompt [$default]: " reply || true
		printf '%s' "${reply:-$default}"
	else
		read -r -p "  $prompt: " reply || true
		printf '%s' "$reply"
	fi
}

# yes_no <prompt> <default y|n>
yes_no() {
	local prompt="$1" default="$2" reply
	local hint="y/N"; [ "$default" = "y" ] && hint="Y/n"
	while true; do
		read -r -p "  $prompt [$hint]: " reply || true
		reply="${reply:-$default}"
		case "${reply,,}" in
			y|yes) return 0 ;;
			n|no)  return 1 ;;
			*) say "  please answer y or n" ;;
		esac
	done
}

# ── preflight ───────────────────────────────────────────────────────────────

say ""
say "${BOLD}VPS Dashboard — setup${RESET}"
say "${DIM}Self-hosted management for a single Linux server.${RESET}"

[ "$(id -u)" -eq 0 ] || die "run this with sudo — the dashboard manages the host, so setup needs root."
[ -f docker-compose.yml ] || die "run this from inside the cloned repository (docker-compose.yml is not here)."

step "Checking what this machine already has"

need_docker=0
if command -v docker >/dev/null 2>&1; then
	ok "docker $(docker --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
else
	warn "docker is not installed"
	need_docker=1
fi

if docker compose version >/dev/null 2>&1; then
	COMPOSE="docker compose"
	ok "docker compose plugin"
elif command -v docker-compose >/dev/null 2>&1; then
	COMPOSE="docker-compose"
	ok "docker-compose (v1)"
else
	[ "$need_docker" -eq 0 ] && warn "docker compose is not installed"
	need_docker=1
	COMPOSE="docker compose"
fi

command -v openssl >/dev/null 2>&1 || die "openssl is required to generate the master key. Install it and re-run."
ok "openssl"

if [ "$need_docker" -eq 1 ]; then
	say ""
	if yes_no "Install Docker now using the official get.docker.com script?" y; then
		step "Installing Docker"
		curl -fsSL https://get.docker.com | sh
		systemctl enable --now docker || true
		docker compose version >/dev/null 2>&1 && COMPOSE="docker compose"
		ok "docker installed"
	else
		die "Docker is required. Install it and re-run this script."
	fi
fi

# ── existing install ────────────────────────────────────────────────────────

if [ -f .env ]; then
	step "An .env already exists"
	warn "It holds VPSD_MASTER_KEY, which encrypts your stored secrets."
	warn "Replacing it makes every stored TOTP seed, database password and"
	warn "backup credential unreadable."
	say ""
	if yes_no "Keep the existing .env and just rebuild?" y; then
		KEEP_ENV=1
	else
		yes_no "${RED}Really overwrite it?${RESET} Existing secrets become unrecoverable" n \
			|| die "nothing changed."
		cp .env ".env.backup.$(date +%s)"
		ok "old .env backed up alongside"
		KEEP_ENV=0
	fi
else
	KEEP_ENV=0
fi

# ── reachability ────────────────────────────────────────────────────────────

if [ "$KEEP_ENV" -eq 0 ]; then

step "How will you reach the dashboard?"
say ""
say "  This dashboard is ${BOLD}root-equivalent${RESET}: anyone who reaches it with a valid"
say "  session effectively has root on this machine. Pick how it is exposed."
say ""

TS_IP=""
WG_IP=""
command -v tailscale >/dev/null 2>&1 && TS_IP="$(tailscale ip -4 2>/dev/null | head -1 || true)"
WG_IF="$(ip -o link show 2>/dev/null | awk -F': ' '/wg[0-9]/{print $2; exit}' || true)"
[ -n "$WG_IF" ] && WG_IP="$(ip -4 -o addr show "$WG_IF" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 || true)"

say "  ${BOLD}1${RESET}) SSH tunnel ${DIM}(safest, nothing is exposed, works from anywhere)${RESET}"
say "  ${BOLD}2${RESET}) Tailscale ${DIM}(reachable from any device on your tailnet)${RESET}${TS_IP:+  ${GREEN}detected: $TS_IP${RESET}}"
say "  ${BOLD}3${RESET}) WireGuard ${DIM}(reachable over your own VPN)${RESET}${WG_IP:+  ${GREEN}detected: $WG_IP${RESET}}"
say "  ${BOLD}4${RESET}) Public IP with an address allowlist ${YELLOW}(not recommended)${RESET}"
say ""

CHOICE="$(ask "Choose 1-4" 1)"

case "$CHOICE" in
1)
	SITE="localhost"
	CIDRS="127.0.0.1/32,::1/128"
	ACCESS_KIND="tunnel"
	;;
2)
	if [ -z "$TS_IP" ]; then
		if command -v tailscale >/dev/null 2>&1; then
			warn "tailscale is installed but this machine is not on a tailnet."
		else
			warn "tailscale is not installed."
		fi
		if yes_no "Install and connect Tailscale now?" y; then
			command -v tailscale >/dev/null 2>&1 || curl -fsSL https://tailscale.com/install.sh | sh
			say ""
			say "  ${BOLD}Follow the URL Tailscale prints to authorise this machine.${RESET}"
			tailscale up
			TS_IP="$(tailscale ip -4 2>/dev/null | head -1 || true)"
		fi
	fi
	[ -n "$TS_IP" ] || die "no Tailscale address available; re-run and pick another option."
	SITE="$TS_IP"
	CIDRS="100.64.0.0/10"
	ACCESS_KIND="tailscale"
	;;
3)
	[ -n "$WG_IP" ] || WG_IP="$(ask "This machine's WireGuard address" "")"
	[ -n "$WG_IP" ] || die "a WireGuard address is required for this option."
	SITE="$WG_IP"
	DEFAULT_WG_NET="$(printf '%s' "$WG_IP" | awk -F. '{print $1"."$2"."$3".0/24"}')"
	CIDRS="$(ask "Which addresses may reach it" "$DEFAULT_WG_NET")"
	ACCESS_KIND="wireguard"
	;;
4)
	say ""
	warn "${YELLOW}This puts a root-equivalent panel on the public internet.${RESET}"
	warn "The address allowlist is the only thing in front of it, and it is"
	warn "checked before authentication — so get it right."
	say ""
	yes_no "Continue anyway?" n || die "sensible. Re-run and pick option 1 or 2."
	PUB_IP="$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1 || true)"
	SITE="$(ask "This machine's public address" "${PUB_IP:-}")"
	[ -n "$SITE" ] || die "an address is required."
	say ""
	say "  ${DIM}Your current address, as seen from here:${RESET} $(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || echo 'could not detect')"
	CIDRS="$(ask "Which addresses may reach it (comma separated, e.g. 203.0.113.7/32)" "")"
	[ -n "$CIDRS" ] || die "an allowlist is required; the backend refuses to start without one."
	ACCESS_KIND="public"
	;;
*)
	die "pick 1, 2, 3 or 4."
	;;
esac

# ── account and behaviour ───────────────────────────────────────────────────

step "First administrator account"

ADMIN_USER="$(ask "Username" admin)"

GENERATED_PW=0
if yes_no "Generate a strong password for it?" y; then
	# 24 characters from a mixed alphabet, then a guaranteed one of each class
	# so it always satisfies the server's own strength rule.
	ADMIN_PW="$(openssl rand -base64 24 | tr -d '/+=' | cut -c1-20)Aa1!"
	GENERATED_PW=1
else
	while true; do
		read -r -s -p "  Password (min 12 chars, mixing 3 of upper/lower/digit/symbol): " ADMIN_PW; echo
		read -r -s -p "  Confirm: " ADMIN_PW2; echo
		[ "$ADMIN_PW" = "$ADMIN_PW2" ] || { warn "they do not match"; continue; }
		[ "${#ADMIN_PW}" -ge 12 ] || { warn "at least 12 characters"; continue; }
		break
	done
fi

step "Behaviour"

REQUIRE_2FA=true
yes_no "Require two-factor authentication? (strongly recommended)" y || REQUIRE_2FA=false
[ "$REQUIRE_2FA" = "false" ] && warn "two-factor disabled — a password alone will be enough"

TERMINAL=true
yes_no "Enable the web terminal? (a real shell with this process's privileges)" y || TERMINAL=false

# ── write .env ──────────────────────────────────────────────────────────────

step "Writing configuration"

MASTER_KEY="$(openssl rand -hex 32)"

umask 077
cat > .env <<EOF
# Written by install.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# This file contains secrets. Keep it at mode 600 and never commit it.

# Encrypts TOTP seeds, database connection strings, deploy env vars and backup
# credentials at rest. Losing it loses every stored secret.
VPSD_MASTER_KEY=$MASTER_KEY

# The single address the stack answers on, and the name on its certificate.
VPSD_SITE=$SITE

# Checked before authentication: an address outside this list cannot even
# reach the login handler.
VPSD_ALLOWED_CIDRS=$CIDRS
VPSD_TRUSTED_PROXIES=127.0.0.1/32
VPSD_ALLOWED_ORIGINS=

VPSD_REQUIRE_2FA=$REQUIRE_2FA
VPSD_TERMINAL_ENABLED=$TERMINAL

# Used once, to create the first account. Safe to remove afterwards.
VPSD_BOOTSTRAP_USER=$ADMIN_USER
VPSD_BOOTSTRAP_PASSWORD=$ADMIN_PW

VPSD_COMPOSE_ROOTS=/opt,/srv,/home
VPSD_GIT_ROOTS=/opt,/srv,/home,/root
VPSD_LOG_ROOTS=/var/log
VPSD_FILE_ROOTS=/
VPSD_BACKUP_DIR=/var/backups/vps-dashboard
VPSD_LOG_LEVEL=info
EOF
chmod 600 .env
ok ".env written (mode 600)"

fi  # KEEP_ENV

# ── build and start ─────────────────────────────────────────────────────────

step "Building and starting the stack"
say "  ${DIM}First build compiles the Go backend and the Next.js frontend; give it a few minutes.${RESET}"
say ""

$COMPOSE up -d --build

step "Waiting for the dashboard to answer"

SITE_ADDR="$(grep -E '^VPSD_SITE=' .env | cut -d= -f2-)"
HEALTHY=0
for _ in $(seq 1 60); do
	if curl -fsS --max-time 3 "http://127.0.0.1:8080/healthz" >/dev/null 2>&1; then
		HEALTHY=1; break
	fi
	sleep 2
done

if [ "$HEALTHY" -eq 1 ]; then
	ok "backend is healthy"
else
	warn "the backend did not answer within two minutes"
	say ""
	say "  Check what it is saying:"
	say "    ${BOLD}$COMPOSE logs backend${RESET}"
	exit 1
fi

# ── how to get in ───────────────────────────────────────────────────────────

say ""
say "${GREEN}${BOLD}The dashboard is running.${RESET}"
say ""

case "${ACCESS_KIND:-tunnel}" in
tunnel)
	say "  ${BOLD}Reach it over an SSH tunnel.${RESET} From your laptop:"
	say ""
	say "    ${BLUE}ssh -L 8443:localhost:8443 $(logname 2>/dev/null || echo root)@$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || echo YOUR_SERVER)${RESET}"
	say ""
	say "  Then open ${BOLD}https://localhost:8443${RESET} while that stays open."
	;;
tailscale)
	say "  ${BOLD}Reach it from any device on your tailnet:${RESET}"
	say ""
	say "    ${BLUE}https://$SITE_ADDR:8443${RESET}"
	;;
wireguard)
	say "  ${BOLD}Reach it over your VPN:${RESET}"
	say ""
	say "    ${BLUE}https://$SITE_ADDR:8443${RESET}"
	;;
public)
	say "  ${BOLD}Reach it at:${RESET}"
	say ""
	say "    ${BLUE}https://$SITE_ADDR:8443${RESET}"
	say ""
	warn "This is on the public internet. Only the addresses you allowlisted"
	warn "can reach it. Consider moving to Tailscale when you get a moment."
	;;
esac

say ""
say "  ${DIM}The certificate is signed by Caddy's own CA, so the browser warns once.${RESET}"
say "  ${DIM}That is expected: the link is already encrypted by the tunnel or VPN.${RESET}"

if [ "${KEEP_ENV:-0}" -eq 0 ]; then
	say ""
	say "  ${BOLD}Sign in with${RESET}"
	say "    username  ${BOLD}$ADMIN_USER${RESET}"
	if [ "${GENERATED_PW:-0}" -eq 1 ]; then
		say "    password  ${BOLD}$ADMIN_PW${RESET}"
		say ""
		warn "Save that password now — it is shown once, and you must change it at first login."
	else
		say "    password  ${DIM}(the one you chose)${RESET}"
	fi
	if [ "$REQUIRE_2FA" = "true" ]; then
		say ""
		say "  You will be asked to enrol an authenticator app before anything else works."
	fi
fi

say ""
say "  ${DIM}Useful from here:${RESET}"
say "    $COMPOSE logs -f backend    ${DIM}# what the server is doing${RESET}"
say "    $COMPOSE restart            ${DIM}# after editing .env${RESET}"
say "    $COMPOSE down               ${DIM}# stop it${RESET}"
say ""
