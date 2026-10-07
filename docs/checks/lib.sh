# Sourced by every check, right after it sets ROOT:
#
#   . "$ROOT/docs/checks/lib.sh"
#
# so that two checks cannot reach into each other.
#
#   - A daemon of its own. Checks shared one daemon, and on one evening a
#     check left it on the uiautomator backend, which every later check
#     inherited, and a sweep's `daemon stop` killed a trace another device
#     was recording. MOBIUM_SESSION names a daemon and its socket; a check
#     gets one unless its caller chose one (a grid run, mobium test), and it
#     is stopped when the check ends, since two daemons driving one device
#     invalidate each other's session on it.
#   - One check per device: check_lock "$DEV" once the device is known, and
#     a second check on the same device is refused, naming the first.
#   - CHECK_TMP, a folder of its own, removed at the end.
#   - at_exit, in place of `trap ... EXIT`: a shell has one EXIT trap, and a
#     check's own would replace this file's. Handlers run last-registered
#     first, before the daemon stops, since they often still need it.
#     at_exit_clear drops the check's handlers — where it once wrote
#     `trap - EXIT` — and keeps this file's.
#
# POSIX sh, as every check is.

if [ -z "${CHECK_LIB:-}" ]; then
CHECK_LIB=1

if [ -z "${MOBIUM_SESSION:-}" ]; then
  # Short: the session names a socket path, which the OS caps at ~104 bytes.
  MOBIUM_SESSION="ck$$"
  CHECK_OWN_DAEMON=1
fi
export MOBIUM_SESSION

CHECK_TMP="$(mktemp -d "${TMPDIR:-/tmp}/mobium-check.XXXXXX")"
CHECK_LOCK=""
_check_handlers=""

at_exit() {
  # Newest first.
  _check_handlers="$1
$_check_handlers"
}

at_exit_clear() {
  _check_handlers=""
}

_check_exit() {
  _check_status=$?
  if [ "$_check_status" = 0 ] && [ -n "$CHECK_SUM" ] && [ "$(cksum < "$CHECK_SELF")" != "$CHECK_SUM" ]; then
    echo "$CHECK_SELF changed while it ran, and sh reads a script as it goes: its result means nothing" >&2
    _check_status=2
  fi
  trap - EXIT INT TERM
  _check_rest="$_check_handlers"
  _check_handlers=""
  while [ -n "$_check_rest" ]; do
    _check_cmd="${_check_rest%%
*}"
    case "$_check_rest" in
      *"
"*) _check_rest="${_check_rest#*
}" ;;
      *) _check_rest="" ;;
    esac
    [ -n "$_check_cmd" ] && eval "$_check_cmd" || true
  done
  if [ -n "${CHECK_OWN_DAEMON:-}" ] && [ -n "${ROOT:-}" ] && [ -x "$ROOT/bin/mobium" ]; then
    "$ROOT/bin/mobium" daemon stop >/dev/null 2>&1 || true
  fi
  [ -n "$CHECK_LOCK" ] && rm -rf "$CHECK_LOCK"
  rm -rf "$CHECK_TMP"
  exit "$_check_status"
}
trap _check_exit EXIT
trap 'exit 130' INT TERM

# A check must parse before it runs. Under set -e, macOS's sh (bash 3.2)
# hands an EXIT trap a syntax error as status 0, and this trap exits with
# the status it is given: audio.sh died at line 266 and reported a pass.
# The checksum catches the other way there: a check edited while it ran.
CHECK_SELF="$0"
CHECK_SUM=""
if [ -f "$CHECK_SELF" ]; then
  sh -n "$CHECK_SELF" || { echo "$CHECK_SELF does not parse, so it was not run" >&2; exit 2; }
  CHECK_SUM=$(cksum < "$CHECK_SELF")
fi

# check_lock DEV takes the device for this check, or refuses: two checks on
# one device fight over its session. A lock whose holder has gone is taken.
#
# A check run by another — third-party-app.sh hands an iPhone to its iOS
# half — shares its caller's lock rather than being refused by it: the
# holder's pid is passed down as CHECK_LOCK_OWNER, and the lock stays the
# caller's to release.
check_lock() {
  _check_dir="${MOBIUM_HOME:-$HOME/.mobium}/locks"
  mkdir -p "$_check_dir"
  _check_name=$(printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_')
  _check_lockdir="$_check_dir/$_check_name"
  if [ -n "${CHECK_LOCK_OWNER:-}" ] && [ "$(cat "$_check_lockdir/pid" 2>/dev/null)" = "$CHECK_LOCK_OWNER" ]; then
    return 0
  fi
  if ! mkdir "$_check_lockdir" 2>/dev/null; then
    _check_pid=$(cat "$_check_lockdir/pid" 2>/dev/null || true)
    if [ -n "$_check_pid" ] && kill -0 "$_check_pid" 2>/dev/null; then
      echo "FAIL: $1 is in use by another check ($(cat "$_check_lockdir/check" 2>/dev/null), pid $_check_pid) — run one check per device at a time" >&2
      exit 2
    fi
    rm -rf "$_check_lockdir"
    mkdir "$_check_lockdir" || { echo "FAIL: could not lock $1" >&2; exit 2; }
  fi
  echo $$ > "$_check_lockdir/pid"
  basename "$0" > "$_check_lockdir/check"
  CHECK_LOCK="$_check_lockdir"
  CHECK_LOCK_OWNER=$$
  export CHECK_LOCK_OWNER
}

fi
