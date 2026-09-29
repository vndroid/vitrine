#!/bin/sh
set -e

# a subcommand (validate, passwd, ...) or a flag (-v, --root ...) as the
# first argument is meant for vitrine, not for a program of the same name
case "$1" in
    validate|passwd|version|help) set -- vitrine "$@" ;;
    -*) set -- vitrine "$@" ;;
esac

exec "$@"
