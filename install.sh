#!/bin/sh
# Installs mad and what it needs to run.
#
#   curl -fsSL https://raw.githubusercontent.com/dcyber-lab/mad/main/install.sh | sh
#
# With options (see usage below, or --help):
#
#   curl -fsSL .../install.sh | sh -s -- --all
#
# What mad needs is installed with the system's package manager (Homebrew,
# apt, dnf, yum, pacman, zypper or apk) and left alone when already there.
# mad itself is the release binary from GitHub, checked against the
# release's checksums.
#
# POSIX sh: `curl | sh` is dash on Debian and Ubuntu.

set -eu

REPO="dcyber-lab/mad"
# The oldest tmux mad runs on, and the one from which nothing is missing.
TMUX_MIN_MAJOR=3
TMUX_MIN_MINOR=0
TMUX_GOOD_MINOR=3

OPTIONAL="${MAD_OPTIONAL:-}"
NO_DEPS="${MAD_NO_DEPS:-}"
DIR="${MAD_INSTALL_DIR:-}"
VERSION="${MAD_VERSION:-}"
DRY_RUN="${MAD_DRY_RUN:-}"

PM=""         # the package manager found
PM_READY=""   # its package lists were refreshed
TMP=""
PROBLEMS=""   # what mad cannot run without, one per line
TODO=""       # what is left for the user to do
NOTES=""

# ---- output ---------------------------------------------------------

if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
	BOLD=$(printf '\033[1m') DIM=$(printf '\033[2m') RED=$(printf '\033[31m')
	YELLOW=$(printf '\033[33m') GREEN=$(printf '\033[32m') OFF=$(printf '\033[0m')
else
	BOLD="" DIM="" RED="" YELLOW="" GREEN="" OFF=""
fi

step() { printf '%s==>%s %s%s%s\n' "$GREEN" "$OFF" "$BOLD" "$*" "$OFF" >&2; }
info() { printf '    %s\n' "$*" >&2; }
warn() { printf '%swarning:%s %s\n' "$YELLOW" "$OFF" "$*" >&2; }
die() {
	printf '%serror:%s %s\n' "$RED" "$OFF" "$*" >&2
	exit 1
}
problem() { PROBLEMS="${PROBLEMS}$*
"; }
todo() { TODO="${TODO}$*
"; }
note() { NOTES="${NOTES}$*
"; }

usage() {
	cat <<'EOF'
Installs mad and what it needs to run: tmux, git, ps and lsof.

usage: install.sh [options]
       curl -fsSL https://raw.githubusercontent.com/dcyber-lab/mad/main/install.sh | sh -s -- [options]

  --all            also what mad can use but does without: lazygit (the
                   diff view), gh (pull requests), notify-send (Linux)
  --no-deps        only mad, no packages
  --dir DIR        where mad goes (default: ~/.local/bin)
  --version TAG    a release such as v0.2.0 (default: the latest)
  --dry-run        say what would be done, change nothing
  -h, --help       this text

The same from the environment: MAD_OPTIONAL=1, MAD_NO_DEPS=1,
MAD_INSTALL_DIR, MAD_VERSION, MAD_DRY_RUN=1.
EOF
}

have() { command -v "$1" >/dev/null 2>&1; }

# works: the command is there and takes what mad gives it, which the ps
# and lsof of BusyBox (Alpine) do not.
works() {
	have "$1" || return 1
	case "$1" in
	ps) ps -axo pid=,tty=,args= >/dev/null 2>&1 ;;
	lsof) lsof -a -p $$ -d cwd -Fn 2>/dev/null | grep -q '^n/' ;;
	esac
}

# run shows a command and runs it, or only shows it with --dry-run.
run() {
	printf '    %s$ %s%s\n' "$DIM" "$*" "$OFF" >&2
	[ -n "$DRY_RUN" ] || "$@"
}

# as_root runs a command as root: directly, or through sudo.
as_root() {
	if [ "$(id -u)" = 0 ]; then
		run "$@"
	elif have sudo; then
		run sudo "$@"
	else
		warn "not root and no sudo: cannot run: $*"
		return 1
	fi
}

cleanup() { [ -z "$TMP" ] || rm -rf "$TMP"; }

# ---- the platform ---------------------------------------------------

detect_platform() {
	case "$(uname -s)" in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	*) die "mad runs on macOS and Linux, not on $(uname -s)" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*) die "no mad release for $(uname -m); build it from source: https://github.com/$REPO#installation" ;;
	esac
}

detect_pm() {
	if [ "$OS" = darwin ]; then
		if have brew; then PM=brew; fi
		return
	fi
	for pm in apt-get dnf yum pacman zypper apk brew; do
		if have "$pm"; then
			PM=$pm
			return
		fi
	done
}

# pkg_name is what a command's package is called by the package manager;
# empty when it has none.
pkg_name() {
	case "$1:$PM" in
	ps:apt-get | ps:zypper | ps:apk) echo procps ;;
	ps:dnf | ps:yum | ps:pacman) echo procps-ng ;;
	ps:brew) echo "" ;; # macOS has it
	gh:pacman | gh:apk) echo github-cli ;;
	notify-send:apt-get) echo libnotify-bin ;;
	notify-send:zypper) echo libnotify-tools ;;
	notify-send:brew) echo "" ;;
	notify-send:*) echo libnotify ;;
	*) echo "$1" ;;
	esac
}

pm_refresh() {
	[ -z "$PM_READY" ] || return 0
	PM_READY=1
	case "$PM" in
	apt-get) as_root apt-get update -qq || true ;;
	pacman) as_root pacman -Sy --noconfirm >/dev/null || true ;;
	apk) as_root apk update -q || true ;;
	esac
}

# pkg_install installs packages; brew refuses to run as root, the others
# need it.
pkg_install() {
	pm_refresh
	case "$PM" in
	brew) run brew install "$@" ;;
	apt-get) as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "$@" ;;
	dnf) as_root dnf install -y -q "$@" ;;
	yum) as_root yum install -y -q "$@" ;;
	pacman) as_root pacman -S --noconfirm --needed "$@" ;;
	zypper) as_root zypper --non-interactive --quiet install "$@" ;;
	apk) as_root apk add -q "$@" ;;
	*) return 1 ;;
	esac
}

pkg_upgrade() {
	pm_refresh
	case "$PM" in
	brew) run brew upgrade "$@" ;;
	apt-get) as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --only-upgrade "$@" ;;
	dnf) as_root dnf upgrade -y -q "$@" ;;
	yum) as_root yum update -y -q "$@" ;;
	pacman) as_root pacman -S --noconfirm "$@" ;;
	zypper) as_root zypper --non-interactive --quiet update "$@" ;;
	apk) as_root apk add -q --upgrade "$@" ;;
	*) return 1 ;;
	esac
}

# how_to_get CMD says how the user gets what could not be installed.
how_to_get() {
	if [ "$OS" = darwin ] && [ -z "$PM" ]; then
		echo "install Homebrew (https://brew.sh), then: brew install $1"
	elif [ -z "$PM" ]; then
		echo "install it with your package manager"
	elif [ "$PM" = brew ]; then
		echo "brew install $(pkg_name "$1") did not work; see above"
	else
		echo "as root, install the package $(pkg_name "$1") with $PM"
	fi
}

# ---- what mad needs -------------------------------------------------

# need installs the commands that are missing; what could not be
# installed is a problem ($1 = required) or a note ($1 = optional).
need() {
	kind=$1
	shift
	missing="" packages=""
	for cmd in "$@"; do
		if works "$cmd"; then
			info "$cmd: $(command -v "$cmd")"
			continue
		fi
		missing="$missing $cmd"
		pkg=$(pkg_name "$cmd")
		[ -z "$pkg" ] || packages="$packages $pkg"
	done
	[ -n "$missing" ] || return 0

	if [ -n "$PM" ] && [ -n "$packages" ]; then
		if [ "$kind" = required ]; then
			# shellcheck disable=SC2086 # a list of words
			pkg_install $packages || true
		else
			# One by one: a distribution without lazygit still gets gh.
			for pkg in $packages; do
				pkg_install "$pkg" || true
			done
		fi
	fi

	for cmd in $missing; do
		if works "$cmd"; then
			info "$cmd: installed"
		elif [ -n "$DRY_RUN" ]; then
			:
		elif [ "$kind" = required ]; then
			problem "$cmd: $(how_to_get "$cmd")"
		else
			note "$cmd could not be installed here; mad works without it"
		fi
	done
}

# tmux_version prints "major minor" of the tmux on PATH, or nothing when
# its version has no number ("tmux master").
tmux_version() {
	tmux -V 2>/dev/null | sed -n 's/^[^0-9]*\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p'
}

tmux_too_old() {
	v=$(tmux_version)
	[ -n "$v" ] || return 1
	major=${v% *} minor=${v#* }
	[ "$major" -lt "$TMUX_MIN_MAJOR" ] || { [ "$major" -eq "$TMUX_MIN_MAJOR" ] && [ "$minor" -lt "$TMUX_MIN_MINOR" ]; }
}

check_tmux() {
	have tmux || return 0 # reported by need
	if tmux_too_old; then
		warn "$(tmux -V) is too old: mad needs tmux $TMUX_MIN_MAJOR.$TMUX_MIN_MINOR or newer"
		if [ -n "$PM" ]; then
			pkg_upgrade tmux || true
		fi
		if [ -z "$DRY_RUN" ] && tmux_too_old; then
			problem "$(tmux -V) is too old and $([ -n "$PM" ] && echo "$PM has no newer one" || echo "there is no package manager to upgrade it"): mad needs tmux $TMUX_MIN_MAJOR.$TMUX_MIN_MINOR or newer (https://github.com/tmux/tmux/wiki/Installing)"
		fi
		return 0
	fi
	v=$(tmux_version)
	[ -n "$v" ] || return 0
	major=${v% *} minor=${v#* }
	if [ "$major" -eq "$TMUX_MIN_MAJOR" ] && [ "$minor" -lt "$TMUX_GOOD_MINOR" ]; then
		note "$(tmux -V) works; from 3.2 on Shift+Enter is a newline in claude, from 3.3 on mad knows whether you are looking"
	fi
}

install_deps() {
	step "What mad needs"
	if [ -z "$PM" ]; then
		if [ "$OS" = darwin ]; then
			warn "no Homebrew: nothing can be installed for you (https://brew.sh)"
		else
			warn "no package manager I know (apt, dnf, yum, pacman, zypper, apk, brew)"
		fi
	else
		info "packages come from $PM"
	fi
	# tmux runs the deck, git tells the projects apart; ps and lsof find
	# the sessions running outside the deck.
	need required tmux git ps lsof
	check_tmux

	if [ -n "$OPTIONAL" ]; then
		step "What mad can use"
		if [ "$OS" = linux ]; then
			need optional lazygit gh notify-send
		else
			need optional lazygit gh
		fi
	fi
}

# ---- mad itself -----------------------------------------------------

# fetch URL FILE
fetch() {
	if have curl; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "neither curl nor wget to download with"
	fi
}

sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	elif have shasum; then
		shasum -a 256 "$1" | cut -d' ' -f1
	elif have openssl; then
		openssl dgst -sha256 "$1" | sed 's/.*= *//'
	fi
}

install_mad() {
	[ -n "$DIR" ] || DIR="$HOME/.local/bin"
	asset="mad_${OS}_${ARCH}.tar.gz"
	if [ -n "$VERSION" ]; then
		base="https://github.com/$REPO/releases/download/$VERSION"
	else
		base="https://github.com/$REPO/releases/latest/download"
	fi

	step "mad ${VERSION:-(the latest release)} for $OS/$ARCH"
	have tar || die "tar is missing: it unpacks the release"
	if [ -n "$DRY_RUN" ]; then
		info "would download $base/$asset"
		info "would install it as $DIR/mad"
		return 0
	fi

	TMP=$(mktemp -d "${TMPDIR:-/tmp}/mad-install.XXXXXX")
	info "downloading $base/$asset"
	fetch "$base/$asset" "$TMP/$asset" || die "could not download $base/$asset"
	fetch "$base/checksums.txt" "$TMP/checksums.txt" || die "could not download $base/checksums.txt"

	want=$(awk -v f="$asset" '$2 == f { print $1 }' "$TMP/checksums.txt")
	got=$(sha256_of "$TMP/$asset")
	if [ -z "$want" ]; then
		die "checksums.txt does not list $asset"
	elif [ -z "$got" ]; then
		warn "no sha256sum, shasum or openssl: the download is not checked"
	elif [ "$want" != "$got" ]; then
		die "$asset is not what was released: sha256 $got, expected $want"
	else
		info "sha256 matches"
	fi

	tar -xzf "$TMP/$asset" -C "$TMP" mad
	chmod 755 "$TMP/mad"
	"$TMP/mad" version >/dev/null || die "the downloaded mad does not run on this machine"

	# Written next to where it goes, then renamed: a deck that is running
	# keeps the mad it has.
	if mkdir -p "$DIR" 2>/dev/null && [ -w "$DIR" ]; then
		cp "$TMP/mad" "$DIR/.mad.new.$$"
		mv -f "$DIR/.mad.new.$$" "$DIR/mad"
	else
		info "$DIR is not yours to write to"
		as_root mkdir -p "$DIR" || die "could not create $DIR"
		as_root cp "$TMP/mad" "$DIR/.mad.new.$$" || die "could not write to $DIR"
		as_root mv -f "$DIR/.mad.new.$$" "$DIR/mad"
	fi
	info "$DIR/mad: $("$DIR/mad" version)"

	first=$(command -v mad 2>/dev/null || true)
	case ":$PATH:" in
	*":$DIR:"*)
		if [ -n "$first" ] && [ "$first" != "$DIR/mad" ]; then
			note "\"mad\" still runs $first ($("$first" version 2>/dev/null || echo "?")): it comes before $DIR on your PATH"
		fi
		;;
	*)
		todo "$DIR is not on your PATH; add to your shell's startup file: export PATH=\"$DIR:\$PATH\""
		if [ -n "$first" ]; then
			todo "  until then \"mad\" runs $first ($("$first" version 2>/dev/null || echo "?"))"
		fi
		;;
	esac
}

check_agents() {
	step "Agents"
	found=""
	for agent in claude codex pi; do
		if have "$agent"; then
			info "$agent: $(command -v "$agent")"
			found=1
		fi
	done
	[ -n "$found" ] && return 0
	info "none of claude, codex, pi is on your PATH"
	note "mad runs agents, and has none to run yet. They are yours to install and sign in to:
    claude  curl -fsSL https://claude.ai/install.sh | bash
    codex   npm install -g @openai/codex
  Until then the deck runs shells."
}

report() {
	echo >&2
	if [ -n "$NOTES" ]; then
		printf '%s' "$NOTES" | while IFS= read -r line; do
			case "$line" in " "*) printf '%s\n' "$line" >&2 ;; *) printf '%snote:%s %s\n' "$BOLD" "$OFF" "$line" >&2 ;; esac
		done
	fi
	if [ -n "$TODO" ]; then
		printf '%s' "$TODO" | while IFS= read -r line; do
			case "$line" in " "*) printf '     %s\n' "$line" >&2 ;; *) printf '%sto do:%s %s\n' "$YELLOW" "$OFF" "$line" >&2 ;; esac
		done
	fi
	if [ -n "$PROBLEMS" ]; then
		printf '%s' "$PROBLEMS" | while IFS= read -r line; do
			printf '%smissing:%s %s\n' "$RED" "$OFF" "$line" >&2
		done
		step "mad is installed, but will not run until the above is there"
		exit 1
	fi
	if [ -n "$DRY_RUN" ]; then
		step "Nothing was changed (--dry-run)"
	else
		step "Done. In a git project, run: mad"
	fi
}

# ---- main -----------------------------------------------------------

main() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--all) OPTIONAL=1 ;;
		--no-deps) NO_DEPS=1 ;;
		--dry-run) DRY_RUN=1 ;;
		--dir)
			[ $# -ge 2 ] || die "--dir needs a directory"
			DIR=$2
			shift
			;;
		--dir=*) DIR=${1#--dir=} ;;
		--version)
			[ $# -ge 2 ] || die "--version needs a tag such as v0.2.0"
			VERSION=$2
			shift
			;;
		--version=*) VERSION=${1#--version=} ;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "unknown option $1 (see --help)" ;;
		esac
		shift
	done
	case "$VERSION" in "" | v*) ;; *) VERSION="v$VERSION" ;; esac

	trap cleanup EXIT
	trap 'exit 130' INT TERM

	detect_platform
	detect_pm
	[ -n "$NO_DEPS" ] || install_deps
	install_mad
	check_agents
	report
}

# The whole script is read before anything runs: a download cut short
# does nothing.
main "$@"
